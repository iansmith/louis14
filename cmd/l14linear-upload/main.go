// Command l14linear-upload uploads files (typically the three render PNGs
// produced for a LOU-114 sub-issue) to Linear's private storage and prints
// the resulting asset URLs, which can be embedded in issue/comment markdown
// as ![alt](assetUrl).
//
// It uses Linear's two-step upload: the fileUpload GraphQL mutation returns a
// pre-signed PUT URL plus the headers that must accompany the PUT.
//
// Auth: set LINEAR_API_KEY to a Linear personal API key (starts with lin_api_).
//
// Usage:
//
//	l14linear-upload <file> [<file>...]
//
// Output: one "path\tassetURL" line per input file, in argument order.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
)

const linearGraphQL = "https://api.linear.app/graphql"

const fileUploadMutation = `mutation FileUpload($contentType: String!, $filename: String!, $size: Int!) {
  fileUpload(contentType: $contentType, filename: $filename, size: $size) {
    success
    uploadFile {
      uploadUrl
      assetUrl
      headers { key value }
    }
  }
}`

type uploadResult struct {
	Data struct {
		FileUpload struct {
			Success    bool `json:"success"`
			UploadFile struct {
				UploadURL string `json:"uploadUrl"`
				AssetURL  string `json:"assetUrl"`
				Headers   []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"headers"`
			} `json:"uploadFile"`
		} `json:"fileUpload"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: l14linear-upload <file> [<file>...]\n\nUploads each file to Linear and prints \"path\\tassetURL\" per line.\nRequires LINEAR_API_KEY (a Linear personal API key).\n")
		os.Exit(1)
	}
	apiKey := os.Getenv("LINEAR_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "LINEAR_API_KEY is not set")
		os.Exit(1)
	}

	exit := 0
	for _, path := range os.Args[1:] {
		assetURL, err := upload(apiKey, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			exit = 1
			continue
		}
		fmt.Printf("%s\t%s\n", path, assetURL)
	}
	os.Exit(exit)
}

func upload(apiKey, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	uf, err := requestUpload(apiKey, contentType, filepath.Base(path), len(data))
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPut, uf.UploadURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Cache-Control", "public, max-age=31536000")
	for _, h := range uf.Headers {
		req.Header.Set(h.Key, h.Value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("PUT to storage: %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return uf.AssetURL, nil
}

type uploadFile struct {
	UploadURL string
	AssetURL  string
	Headers   []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
}

func requestUpload(apiKey, contentType, filename string, size int) (*uploadFile, error) {
	payload, err := json.Marshal(map[string]any{
		"query": fileUploadMutation,
		"variables": map[string]any{
			"contentType": contentType,
			"filename":    filename,
			"size":        size,
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, linearGraphQL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("fileUpload mutation: %s: %s", resp.Status, bytes.TrimSpace(body))
	}

	var result uploadResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("fileUpload mutation: %s", result.Errors[0].Message)
	}
	if !result.Data.FileUpload.Success {
		return nil, fmt.Errorf("fileUpload mutation: success=false")
	}

	uf := result.Data.FileUpload.UploadFile
	out := &uploadFile{UploadURL: uf.UploadURL, AssetURL: uf.AssetURL}
	out.Headers = uf.Headers
	return out, nil
}
