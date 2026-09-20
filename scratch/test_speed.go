package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// 1. Test Ping / Latency
	t0 := time.Now()
	respPing, err := client.Get("https://speed.cloudflare.com/__down?bytes=0")
	if err != nil {
		fmt.Printf("Ping error: %v\n", err)
		return
	}
	respPing.Body.Close()
	pingMs := float64(time.Since(t0).Microseconds()) / 1000.0
	fmt.Printf("Ping: %.2f ms\n", pingMs)

	// 2. Test Download (5 MB)
	const dlBytes = 5 * 1024 * 1024
	t1 := time.Now()
	respDl, err := client.Get(fmt.Sprintf("https://speed.cloudflare.com/__down?bytes=%d", dlBytes))
	if err != nil {
		fmt.Printf("Download error: %v\n", err)
		return
	}
	n, err := io.Copy(io.Discard, respDl.Body)
	respDl.Body.Close()
	dlSec := time.Since(t1).Seconds()
	dlMbps := (float64(n) * 8.0) / (dlSec * 1000000.0)
	fmt.Printf("Download: %.2f Mbps (received %d bytes in %.2fs)\n", dlMbps, n, dlSec)

	// 3. Test Upload (1 MB)
	upBuf := bytes.Repeat([]byte("0123456789abcdef"), 64*1024) // 1 MB
	t2 := time.Now()
	respUp, err := client.Post("https://speed.cloudflare.com/__up", "application/octet-stream", bytes.NewReader(upBuf))
	if err != nil {
		fmt.Printf("Upload error: %v\n", err)
		return
	}
	io.Copy(io.Discard, respUp.Body)
	respUp.Body.Close()
	upSec := time.Since(t2).Seconds()
	upMbps := (float64(len(upBuf)) * 8.0) / (upSec * 1000000.0)
	fmt.Printf("Upload: %.2f Mbps (sent %d bytes in %.2fs)\n", upMbps, len(upBuf), upSec)
}
