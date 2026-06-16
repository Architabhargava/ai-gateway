package compliance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProhibitedResult holds the verdict from the Article 5 prohibited use detector
type ProhibitedResult struct {
	IsProhibited       bool     `json:"is_prohibited"`
	Article            string   `json:"article"`
	ArticleDescription string   `json:"article_description"`
	LegalReference     string   `json:"legal_reference"`
	Category           string   `json:"category"`
	Reason             string   `json:"reason"`
	Confidence         float64  `json:"confidence"`
	Indicators         []string `json:"indicators"`
}

// Detector checks prompts against EU AI Act Article 5 prohibited use cases
type Detector struct {
	groqKey string
}

// NewDetector creates a prohibited use case detector
func NewDetector(groqKey string) *Detector {
	fmt.Println("[Prohibited] Article 5 detector initialised")
	return &Detector{groqKey: groqKey}
}

// ArticleReference returns the precise EUR-Lex URL and plain English
// description for each EU AI Act article cited in a block decision
func ArticleReference(article string) (url string, description string) {
	base := "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689"

	references := map[string]struct {
		anchor string
		desc   string
	}{
		"Article 5(1)(a)": {
			anchor: "#d1e2816-1-1",
			desc:   "Subliminal manipulation techniques that distort behaviour without awareness",
		},
		"Article 5(1)(b)": {
			anchor: "#d1e2816-1-1",
			desc:   "Exploitation of vulnerabilities of specific groups (age, disability, social situation)",
		},
		"Article 5(1)(c)": {
			anchor: "#d1e2816-1-1",
			desc:   "Social scoring by public authorities leading to detrimental treatment",
		},
		"Article 5(1)(d)": {
			anchor: "#d1e2816-1-1",
			desc:   "Real-time remote biometric identification in publicly accessible spaces",
		},
		"Article 5(1)(e)": {
			anchor: "#d1e2816-1-1",
			desc:   "Untargeted scraping of facial images to build recognition databases",
		},
		"Article 5(1)(f)": {
			anchor: "#d1e2816-1-1",
			desc:   "Emotion recognition in workplaces or educational institutions",
		},
		"Article 5(1)(g)": {
			anchor: "#d1e2816-1-1",
			desc:   "Predictive policing based solely on profiling of natural persons",
		},
		"Article 9": {
			anchor: "#d1e3622-1-1",
			desc:   "Risk management system obligations for high-risk AI systems",
		},
		"Article 13": {
			anchor: "#d1e4118-1-1",
			desc:   "Transparency and provision of information to deployers",
		},
		"Article 52": {
			anchor: "#d1e7542-1-1",
			desc:   "Transparency obligations for certain AI systems interacting with humans",
		},
	}

	if ref, ok := references[article]; ok {
		return base + ref.anchor, ref.desc
	}

	return base, "EU AI Act — general reference"
}

// Check runs the prompt against all Article 5 prohibited categories.
// Two-stage approach:
//  1. Fast keyword pre-screen for obvious cases (no AI call needed)
//  2. AI deep check via Groq for nuanced and indirect prohibited uses
//
// Fails open — if Groq is unavailable, returns not prohibited.
func (d *Detector) Check(prompt string) ProhibitedResult {
	safe := ProhibitedResult{
		IsProhibited:       false,
		Article:            "",
		ArticleDescription: "",
		LegalReference:     "",
		Category:           "not_prohibited",
		Reason:             "no Article 5 violations detected",
		Confidence:         0.0,
		Indicators:         []string{},
	}

	// Stage 1 — fast keyword pre-screen
	// High-precision patterns that almost never appear in legitimate prompts
	type fastMatch struct {
		article string
		pattern string
	}
	fastPatterns := []fastMatch{
		{"Article 5(1)(c)", "social credit score"},
		{"Article 5(1)(c)", "social scoring system"},
		{"Article 5(1)(c)", "citizen score"},
		{"Article 5(1)(c)", "citizen ranking"},
		{"Article 5(1)(d)", "biometric surveillance"},
		{"Article 5(1)(d)", "facial recognition crowd"},
		{"Article 5(1)(d)", "real-time facial recognition public"},
		{"Article 5(1)(e)", "bulk facial recognition"},
		{"Article 5(1)(e)", "scrape facial images"},
		{"Article 5(1)(e)", "harvest face data"},
		{"Article 5(1)(a)", "subliminal manipulation"},
		{"Article 5(1)(a)", "subconscious manipulation"},
		{"Article 5(1)(a)", "subliminal technique"},
		{"Article 5(1)(b)", "exploit vulnerability children"},
		{"Article 5(1)(b)", "exploit elderly"},
		{"Article 5(1)(d)", "predictive policing profile"},
		{"Article 5(1)(d)", "predict crime profile"},
		{"Article 5(1)(f)", "emotion recognition workplace"},
		{"Article 5(1)(f)", "emotion recognition school"},
		{"Article 5(1)(f)", "monitor employee emotions"},
		{"Article 5(1)(f)", "detect student emotions"},
	}

	lower := strings.ToLower(prompt)
	for _, fp := range fastPatterns {
		if strings.Contains(lower, fp.pattern) {
			refURL, refDesc := ArticleReference(fp.article)
			fmt.Printf("[Prohibited] Fast pre-screen match — pattern: %q article: %s\n",
				fp.pattern, fp.article)
			return ProhibitedResult{
				IsProhibited:       true,
				Article:            fp.article,
				ArticleDescription: refDesc,
				LegalReference:     refURL,
				Category:           "fast_screen_match",
				Reason:             fmt.Sprintf("Prompt contains pattern directly associated with EU AI Act prohibited use: %q", fp.pattern),
				Confidence:         0.98,
				Indicators:         []string{fp.pattern},
			}
		}
	}

	// Stage 2 — AI deep check for nuanced prohibited use attempts
	if d.groqKey == "" {
		fmt.Println("[Prohibited] No Groq key — skipping AI prohibited use check")
		return safe
	}

	return d.aiCheck(prompt, safe)
}

// aiCheck sends the prompt to Groq with a focused Article 5 system prompt
func (d *Detector) aiCheck(prompt string, safe ProhibitedResult) ProhibitedResult {
	systemPrompt := `You are a legal compliance classifier specialising in the EU AI Act Article 5 prohibited AI practices.

Your ONLY job: determine if the user prompt is asking for assistance with, instructions for, or a system design implementing a use case explicitly PROHIBITED under EU AI Act Article 5.

## EU AI Act Article 5 — Prohibited practices

### Article 5(1)(a) — Subliminal manipulation
AI that deploys subliminal techniques beyond a person's consciousness to materially distort behaviour causing harm.
Examples: hidden audio frequencies, imperceptible visual cues, psychological manipulation to alter decisions without awareness.

### Article 5(1)(b) — Exploitation of vulnerabilities
AI that exploits vulnerabilities of specific groups (age, disability, social/economic situation) to materially distort behaviour causing harm.
Examples: targeting elderly with manipulative content, exploiting children's developmental stage, manipulating people in financial distress.

### Article 5(1)(c) — Social scoring by public authorities
AI used by public authorities to evaluate trustworthiness of persons based on social behaviour, leading to detrimental treatment.
Examples: citizen social credit systems, government behaviour scoring, public trustworthiness ratings.

### Article 5(1)(d) — Real-time biometric identification in public spaces
Real-time remote biometric identification in publicly accessible spaces for law enforcement.
Examples: live facial recognition in streets for police, real-time crowd identity scanning.

### Article 5(1)(e) — Untargeted facial image scraping
Untargeted scraping of facial images from internet or CCTV to build facial recognition databases.
Examples: bulk scraping social media for faces, building face databases from CCTV footage.

### Article 5(1)(f) — Emotion recognition at work or school
AI for emotion recognition in workplaces or educational institutions (except for safety reasons).
Examples: monitoring employee emotional states for productivity, scanning student faces for engagement.

### Article 5(1)(g) — Predictive policing based on profiling
AI for risk assessments to predict future criminal offences based solely on profiling.
Examples: predicting criminality from demographic data, pre-crime scoring from social profile.

## What is NOT prohibited — do NOT flag these
- Academic research ABOUT these topics
- How the EU AI Act works or what it prohibits
- Security research discussing these threats
- Journalism investigating these practices
- Building systems to DETECT or PREVENT these practices
- General questions about surveillance, biometrics, or AI ethics
- Historical or policy analysis of social scoring systems
- Explaining what emotion recognition technology is

## Reasoning steps
Step 1: What is this prompt literally asking to build or implement?
Step 2: Does it request implementation help for one of the above prohibited categories?
Step 3: Or is it academic, journalistic, defensive, or educational in nature?
Step 4: Final verdict with confidence.

## Output
Return ONLY raw JSON. No markdown. No code fences. Nothing outside the JSON.

{
  "is_prohibited": <true|false>,
  "article": <exact EU AI Act article e.g. "Article 5(1)(c)" or "" if not prohibited>,
  "category": <"subliminal_manipulation"|"vulnerability_exploitation"|"social_scoring"|"biometric_surveillance"|"facial_scraping"|"emotion_recognition"|"predictive_policing"|"not_prohibited">,
  "reason": <one sentence explaining the verdict>,
  "confidence": <float 0.0 to 1.0>,
  "indicators": <array of exact phrases from the prompt that indicate prohibited use, empty array if not prohibited>
}`

	payload := map[string]interface{}{
		"model": "llama-3.3-70b-versatile",
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": "Check this prompt for Article 5 violations:\n\n" + prompt},
		},
		"temperature": 0.0,
		"max_tokens":  300,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("[Prohibited] Failed to marshal request:", err)
		return safe
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.groq.com/openai/v1/chat/completions",
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		fmt.Println("[Prohibited] Failed to build request:", err)
		return safe
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.groqKey)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println("[Prohibited] HTTP error — failing open:", err)
		return safe
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("[Prohibited] Failed to read response:", err)
		return safe
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[Prohibited] Groq returned HTTP %d — failing open\n", resp.StatusCode)
		return safe
	}

	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil || len(envelope.Choices) == 0 {
		fmt.Println("[Prohibited] Failed to parse Groq envelope — failing open")
		return safe
	}

	content := strings.TrimSpace(envelope.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start == -1 || end == -1 || end <= start {
		fmt.Printf("[Prohibited] No JSON in response: %q\n", content)
		return safe
	}
	content = content[start : end+1]

	var result ProhibitedResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		fmt.Printf("[Prohibited] JSON unmarshal failed: %v\n", err)
		return safe
	}

	if result.Indicators == nil {
		result.Indicators = []string{}
	}

	// Enrich with precise article reference
	if result.IsProhibited && result.Article != "" {
		result.LegalReference, result.ArticleDescription = ArticleReference(result.Article)
	}

	fmt.Printf("[Prohibited] Check complete — prohibited=%v article=%q category=%s confidence=%.2f\n",
		result.IsProhibited, result.Article, result.Category, result.Confidence)

	return result
}
