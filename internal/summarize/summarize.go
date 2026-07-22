package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Summary string
	Tags    []string
}

type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
	// RetryBackoff is the base wait between retries on a busy (503/429) server;
	// zero disables the wait (tests). Backoff grows linearly per attempt.
	RetryBackoff time.Duration
}

// maxRetries bounds retries on a busy server so one wedged endpoint can't stall
// a whole run indefinitely.
const maxRetries = 5

func FromEnv() *Client {
	url := os.Getenv("GLANE_SUMMARY_URL")
	if url == "" {
		return nil
	}
	// A hard timeout that fires mid-generation disconnects the client, which on a
	// single-slot local server (e.g. MLX) leaves the aborted generation occupying
	// the slot and every following request gets 503. Default generously; override
	// with GLANE_SUMMARY_TIMEOUT (seconds) if your model/articles need more.
	timeout := 180 * time.Second
	if v := os.Getenv("GLANE_SUMMARY_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	return &Client{
		BaseURL:      url,
		Model:        os.Getenv("GLANE_SUMMARY_MODEL"),
		APIKey:       os.Getenv("GLANE_SUMMARY_KEY"),
		HTTP:         &http.Client{Timeout: timeout},
		RetryBackoff: 2 * time.Second,
	}
}

const systemPrompt = `You summarize and categorize technical articles. Reply with ONLY a JSON object, no prose and no code fences: {"summary": "a 2-3 sentence summary for a technical reader focusing on the key takeaway", "tags": ["3-6 lowercase topic or technology tags"]}`

func (c *Client) Summarize(ctx context.Context, title, article string, knownTags []string) (Result, error) {
	system := systemPrompt
	if len(knownTags) > 0 {
		system += "\nPrefer reusing these existing tags when they fit; only invent a new tag when none apply: " + strings.Join(knownTags, ", ")
	}
	body, _ := json.Marshal(map[string]any{
		"model": c.Model,
		// A summary + tags is short. Without a cap, OpenAI-compatible servers reserve
		// a full-context KV block per request; on memory-bound local backends (e.g.
		// MLX) that projected reservation exceeds the GPU cap and the request is
		// rejected with 503 before it's even enqueued.
		"max_tokens": 512,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": title + "\n\n" + cutRunes(article, 8000)},
		},
	})
	resp, err := c.do(ctx, body)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("summary: status %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, err
	}
	if len(out.Choices) == 0 {
		return Result{}, fmt.Errorf("summary: no choices")
	}
	var parsed struct {
		Summary string   `json:"summary"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(extractJSON(out.Choices[0].Message.Content)), &parsed); err != nil {
		return Result{}, fmt.Errorf("summary: bad JSON: %w", err)
	}
	if parsed.Summary == "" {
		return Result{}, fmt.Errorf("summary: empty summary")
	}
	return Result{Summary: parsed.Summary, Tags: cleanTags(parsed.Tags)}, nil
}

// do POSTs the chat request, retrying on a busy server (503/429) with a linear
// backoff. Other statuses and network errors return immediately — only "come
// back later" is worth retrying.
func (c *Client) do(ctx context.Context, body []byte) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		req, rerr := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
		if rerr != nil {
			return nil, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		resp, err = c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusServiceUnavailable && resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		resp.Body.Close()
		if attempt >= maxRetries {
			return nil, fmt.Errorf("summary: server busy (status %d) after %d retries", resp.StatusCode, maxRetries)
		}
		select {
		case <-time.After(time.Duration(attempt+1) * c.RetryBackoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// extractJSON returns the substring from the first '{' to the last '}', so a
// model that wraps the object in prose or ```json fences still parses.
func extractJSON(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func cleanTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
