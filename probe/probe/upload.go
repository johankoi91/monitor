package probe

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/johankoi91/monitor/runtime/shared/protocol"
	"io"
	"net/http"
	"strconv"
)

func (c *Client) uploadBundle(ctx context.Context, client *http.Client, request protocol.LogRequest, data []byte) error {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	base := c.UploadURL(request.ID)
	send := func(method, url string, body []byte, chunk bool) (int, []byte, error) {
		r, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		r.SetBasicAuth(c.identity.ID, c.identity.Secret)
		r.Header.Set("X-Bundle-SHA256", digest)
		r.Header.Set("X-Bundle-Bytes", strconv.Itoa(len(data)))
		if chunk {
			h := sha256.Sum256(body)
			r.Header.Set("X-Content-SHA256", hex.EncodeToString(h[:]))
		} else {
			r.Header.Set("X-Content-SHA256", digest)
		}
		r.Header.Set("Content-Type", "application/octet-stream")
		response, err := client.Do(r)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		b, err := io.ReadAll(io.LimitReader(response.Body, 32769))
		if len(b) > 32768 {
			return response.StatusCode, nil, fmt.Errorf("oversized upload response")
		}
		return response.StatusCode, b, err
	}
	status, b, err := send("GET", base+"/parts", nil, false)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("upload resume status %d", status)
	}
	var progress struct {
		Completed  bool  `json:"completed"`
		Missing    []int `json:"missing"`
		ChunkBytes int   `json:"chunk_bytes"`
	}
	if err = json.Unmarshal(b, &progress); err != nil {
		return err
	}
	if progress.Completed {
		return nil
	}
	count := (len(data) + protocol.LogChunkBytes - 1) / protocol.LogChunkBytes
	if progress.ChunkBytes != protocol.LogChunkBytes || len(progress.Missing) > count {
		return fmt.Errorf("invalid chunk negotiation")
	}
	seen := map[int]bool{}
	for _, index := range progress.Missing {
		if index < 0 || index >= count || seen[index] {
			return fmt.Errorf("invalid missing chunk index")
		}
		seen[index] = true
		end := (index + 1) * protocol.LogChunkBytes
		if end > len(data) {
			end = len(data)
		}
		status, _, err = send("PUT", base+"/parts/"+strconv.Itoa(index), data[index*protocol.LogChunkBytes:end], true)
		if err != nil {
			return err
		}
		if status/100 != 2 {
			return fmt.Errorf("chunk upload status %d", status)
		}
	}
	status, _, err = send("POST", base+"/complete", nil, false)
	if err != nil {
		return err
	}
	if status/100 != 2 {
		return fmt.Errorf("upload completion status %d", status)
	}
	return nil
}
