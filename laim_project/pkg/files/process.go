package files

import (
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"code.sajari.com/docconv"
)

type OllamaInput struct {
	TextContent string
	ImageBytes  [][]byte
	Filename    string
	MimeType    string
}

func isBinary(b []byte) bool {
	check := b
	if len(check) > 512 {
		check = check[:512]
	}
	if strings.Contains(string(check), "\x00") {
		return true
	}
	return !utf8.Valid(check)
}

func ProcessSingleFile(file multipart.File, header *multipart.FileHeader, r *http.Request) (OllamaInput, error) {
	all, err := io.ReadAll(file)
	if err != nil {
		return OllamaInput{}, err
	}

	mime := http.DetectContentType(all)
	ext := strings.ToLower(filepath.Ext(header.Filename))

	out := OllamaInput{
		Filename: header.Filename,
		MimeType: mime,
	}

	if strings.HasPrefix(mime, "image/") {
		out.ImageBytes = [][]byte{all}
		return out, nil
	}

	switch ext {
	case ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".odt", ".rtf", ".html":
		res, err := docconv.Convert(strings.NewReader(string(all)), ext, false)
		if err == nil {
			out.TextContent = res.Body
			return out, nil
		}
	}

	if isBinary(all) {
		out.TextContent = "[WARNING: binary file]\n" + string(all)
	} else {
		out.TextContent = string(all)
	}

	return out, nil
}
