package uploads

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

type Result struct {
	URL    string `json:"url"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

func SavePrivate(root string, file multipart.File, header *multipart.FileHeader, kind string, max int64) (Result, error) {
	if header.Size > max {
		return Result{}, fmt.Errorf("file exceeds the allowed size")
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if len(ext) > 12 {
		ext = ""
	}
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	name := hex.EncodeToString(buf) + ext
	dir := filepath.Join(root, "data", "private", kind)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return Result{}, err
	}
	target := filepath.Join(dir, name)
	out, err := os.Create(target)
	if err != nil {
		return Result{}, err
	}
	defer out.Close()
	hash := sha256.New()
	n, err := io.CopyN(io.MultiWriter(out, hash), file, max+1)
	if err != nil && err != io.EOF {
		return Result{}, err
	}
	if n > max {
		_ = os.Remove(target)
		return Result{}, fmt.Errorf("file exceeds the allowed size")
	}
	mediaType := "file"
	ct := header.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/") {
		mediaType = "image"
	} else if strings.HasPrefix(ct, "audio/") {
		mediaType = "audio"
	} else if strings.HasPrefix(ct, "video/") {
		mediaType = "video"
	}
	return Result{URL: "/api/private-media/" + kind + "/" + name, Name: filepath.Base(header.Filename), Type: mediaType, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func SavePublic(root string, file multipart.File, header *multipart.FileHeader, folder string, max int64) (Result, error) {
	if header.Size > max {
		return Result{}, fmt.Errorf("file exceeds the allowed size")
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if len(ext) > 12 {
		ext = ""
	}
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	name := hex.EncodeToString(buf) + ext
	if folder == "" {
		folder = "files"
	}
	dir := filepath.Join(root, "data", "library", folder)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return Result{}, err
	}
	target := filepath.Join(dir, name)
	out, err := os.Create(target)
	if err != nil {
		return Result{}, err
	}
	defer out.Close()
	hash := sha256.New()
	n, err := io.CopyN(io.MultiWriter(out, hash), file, max+1)
	if err != nil && err != io.EOF {
		return Result{}, err
	}
	if n > max {
		_ = os.Remove(target)
		return Result{}, fmt.Errorf("file exceeds the allowed size")
	}
	mediaType := "file"
	ct := header.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/") {
		mediaType = "image"
	} else if strings.HasPrefix(ct, "audio/") {
		mediaType = "audio"
	} else if strings.HasPrefix(ct, "video/") {
		mediaType = "video"
	}
	return Result{URL: "/library/" + folder + "/" + name, Name: filepath.Base(header.Filename), Type: mediaType, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}
