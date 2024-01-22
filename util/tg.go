package util

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type fileIdRequest struct {
	Ok     bool `json:"ok"`
	Result struct {
		FileID       string `json:"file_id"`
		FileUniqueID string `json:"file_unique_id"`
		FileSize     int    `json:"file_size"`
		FilePath     string `json:"file_path"`
	} `json:"result"`
}

// downloading file from url, then send it to telegram bot
func DownloadTgFileToLocal(ctx context.Context, tgtoken string, tgFileId string, filepath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf(
		"https://api.telegram.org/bot%s/getFile?file_id=%s", tgtoken, tgFileId), nil)
	if err != nil {
		return fmt.Errorf("failed to create request for tg file meta: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch tg file meta: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to fetch tg file meta data: %v", err)
	}
	var responseTeleFile fileIdRequest
	err = json.Unmarshal(body, &responseTeleFile)
	if err != nil {
		return fmt.Errorf("failed to parse tg file meta: %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", tgtoken,
		responseTeleFile.Result.FilePath))
	if err != nil {
		return fmt.Errorf("failed to fetch tg file: %v", err)
	}
	defer resp.Body.Close()
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create local file '%s': %v", filepath, err)
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}
