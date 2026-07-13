package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Client struct {
	BaseURL string
	Model   string
	APIKey  string
	HTTP    *http.Client
}

func FromEnv() *Client {
	url := os.Getenv("GLANE_EMBED_URL")
	if url == "" {
		return nil // semantic disabled
	}
	return &Client{
		BaseURL: url,
		Model:   os.Getenv("GLANE_EMBED_MODEL"),
		APIKey:  os.Getenv("GLANE_EMBED_KEY"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embeddings: status %d", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
