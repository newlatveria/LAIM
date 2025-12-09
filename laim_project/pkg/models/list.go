package models

import (
	"context"
	"net/http"
	"net/url"

	"github.com/ollama/ollama/api"
)

func ListLocalModels(r *http.Request) ([]string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)
	res, err := client.List(context.Background())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range res.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
