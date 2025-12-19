package ollama

import (
	"encoding/json"
	"net/http"
)

func (c *Client) ListModels(w http.ResponseWriter) {
	resp, err := c.HTTP.Get(c.Cfg.TagsURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	json.NewDecoder(resp.Body).Decode(w)
}
