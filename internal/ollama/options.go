package ollama

func buildOptions(params GenerationParams) map[string]interface{} {
	opts := make(map[string]interface{})

	if params.Temperature > 0 {
		opts["temperature"] = params.Temperature
	}
	if params.TopP > 0 {
		opts["top_p"] = params.TopP
	}
	if params.TopK > 0 {
		opts["top_k"] = params.TopK
	}
	if params.RepeatPenalty > 0 {
		opts["repeat_penalty"] = params.RepeatPenalty
	}
	if params.NumPredict > 0 {
		opts["num_predict"] = params.NumPredict
	}

	return opts
}
