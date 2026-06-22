package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ai-gateway/internal/logger"
)

// HandleMetricsAPI returns aggregated metrics data for the platform dashboard.
// Called every 10 seconds by the metrics panel via fetch().
func (g *Gateway) HandleMetricsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	logs, err := g.logger.GetAll()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
		return
	}

	// ── Summary counts ────────────────────────────────────────────────────
	summary := map[string]int{
		"total":        0,
		"allowed":      0,
		"blocked":      0,
		"review":       0,
		"error":        0,
		"high_risk":    0,
		"unacceptable": 0,
	}

	// ── Blocked by category ───────────────────────────────────────────────
	categoryCount := map[string]int{}

	// ── Risk level breakdown ──────────────────────────────────────────────
	riskCount := map[string]int{
		"minimal":      0,
		"limited":      0,
		"high":         0,
		"unacceptable": 0,
	}

	// ── Hourly buckets for last 24 hours ──────────────────────────────────
	now := time.Now()
	cutoff24h := now.Add(-24 * time.Hour)

	type hourBucket struct {
		Hour    string `json:"hour"`
		Allowed int    `json:"allowed"`
		Blocked int    `json:"blocked"`
	}
	hourMap := map[string]*hourBucket{}

	// ── Last 7 days daily buckets ─────────────────────────────────────────
	cutoff7d := now.Add(-7 * 24 * time.Hour)
	type dayBucket struct {
		Day     string `json:"day"`
		Total   int    `json:"total"`
		Blocked int    `json:"blocked"`
	}
	dayMap := map[string]*dayBucket{}

	// ── Classifier score distribution ────────────────────────────────────
	scoreRanges := map[string]int{
		"0.0-0.2": 0,
		"0.2-0.4": 0,
		"0.4-0.6": 0,
		"0.6-0.8": 0,
		"0.8-1.0": 0,
	}

	for _, l := range logs {
		summary["total"]++

		switch l.Status {
		case "allowed":
			summary["allowed"]++
		case "blocked":
			summary["blocked"]++
		case "review_pending":
			summary["review"]++
		case "error":
			summary["error"]++
		}

		switch l.RiskLevel {
		case logger.RiskHigh:
			summary["high_risk"]++
			riskCount["high"]++
		case logger.RiskUnacceptable:
			summary["unacceptable"]++
			riskCount["unacceptable"]++
		case logger.RiskLimited:
			riskCount["limited"]++
		default:
			riskCount["minimal"]++
		}

		// Category counts for blocked requests
		if l.Blocked && l.Category != "" && l.Category != "safe" {
			categoryCount[l.Category]++
		}

		// Hourly buckets
		if l.Timestamp.After(cutoff24h) {
			hour := l.Timestamp.Format("15:00")
			if _, ok := hourMap[hour]; !ok {
				hourMap[hour] = &hourBucket{Hour: hour}
			}
			if l.Status == "allowed" {
				hourMap[hour].Allowed++
			} else if l.Status == "blocked" {
				hourMap[hour].Blocked++
			}
		}

		// Daily buckets
		if l.Timestamp.After(cutoff7d) {
			day := l.Timestamp.Format("Mon 02")
			if _, ok := dayMap[day]; !ok {
				dayMap[day] = &dayBucket{Day: day}
			}
			dayMap[day].Total++
			if l.Blocked {
				dayMap[day].Blocked++
			}
		}

		// Score distribution
		score := l.ClassifierScore
		switch {
		case score < 0.2:
			scoreRanges["0.0-0.2"]++
		case score < 0.4:
			scoreRanges["0.2-0.4"]++
		case score < 0.6:
			scoreRanges["0.4-0.6"]++
		case score < 0.8:
			scoreRanges["0.6-0.8"]++
		default:
			scoreRanges["0.8-1.0"]++
		}
	}

	// Convert hourMap to sorted slice for last 24 hours
	var hourlyData []hourBucket
	for i := 23; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * time.Hour)
		hour := t.Format("15:00")
		if b, ok := hourMap[hour]; ok {
			hourlyData = append(hourlyData, *b)
		} else {
			hourlyData = append(hourlyData, hourBucket{Hour: hour})
		}
	}

	// Convert dayMap to sorted slice for last 7 days
	var dailyData []dayBucket
	for i := 6; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * 24 * time.Hour)
		day := t.Format("Mon 02")
		if b, ok := dayMap[day]; ok {
			dailyData = append(dailyData, *b)
		} else {
			dailyData = append(dailyData, dayBucket{Day: day})
		}
	}

	// Block rate percentage
	blockRate := 0.0
	if summary["total"] > 0 {
		blockRate = float64(summary["blocked"]) / float64(summary["total"]) * 100
	}

	// Average classifier score
	avgScore := 0.0
	if len(logs) > 0 {
		total := 0.0
		for _, l := range logs {
			total += l.ClassifierScore
		}
		avgScore = total / float64(len(logs))
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"summary":      summary,
		"block_rate":   fmt.Sprintf("%.1f", blockRate),
		"avg_score":    fmt.Sprintf("%.2f", avgScore),
		"by_category":  categoryCount,
		"by_risk":      riskCount,
		"hourly":       hourlyData,
		"daily":        dailyData,
		"score_dist":   scoreRanges,
		"generated_at": now.Format("2006-01-02 15:04:05"),
	})
}
