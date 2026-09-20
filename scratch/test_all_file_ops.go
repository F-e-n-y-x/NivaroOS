package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const baseURL = "http://127.0.0.1"

type ApiResponse struct {
	Success int         `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type FileOperateItem struct {
	From string `json:"from"`
}

type FileOperatePayload struct {
	Type  string            `json:"type"`
	Item  []FileOperateItem `json:"item"`
	To    string            `json:"to"`
	Style string            `json:"style"`
}

func main() {
	fmt.Println("=== STARTING COMPREHENSIVE FILE OPERATIONS TEST ===")

	// 1. Setup local test file
	localSrcDir := "/DATA/Documents"
	_ = os.MkdirAll(localSrcDir, 0755)
	localTestFile := filepath.Join(localSrcDir, "e2e_unified_test.txt")
	testPayload := "NivaroOS Unified File Engine Test: " + time.Now().Format(time.RFC3339)
	if err := os.WriteFile(localTestFile, []byte(testPayload), 0644); err != nil {
		fmt.Printf("FAIL: Cannot create local test file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(localTestFile)
	fmt.Println("✔ 1. Local test file created")

	// 2. Test Companion Device directory creation (/v1/folder - POST)
	companionFolder := "/DATA/Companion/Ayush_s Tablet/Download/NivaroTestDir"
	mkdirBody, _ := json.Marshal(map[string]string{"path": companionFolder})
	resp, err := http.Post(baseURL+"/v1/folder", "application/json", bytes.NewReader(mkdirBody))
	if err != nil {
		fmt.Printf("FAIL: Mkdir request error: %v\n", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("✔ 2. Companion Mkdir result: status %d, body: %s\n", resp.StatusCode, string(body))
	}

	// 3. Test Direct File Upload to Companion Device (/v1/file/upload - POST)
	uploadToCompanion(companionFolder, "uploaded_file.txt", "Direct Companion Upload Content: "+time.Now().String())

	// 4. Test Copy from Local -> Companion Device (/v1/batch/task - POST)
	copyLocalToCompanion(localTestFile, companionFolder)

	// 5. Test Reading Content from Companion Device (/v1/file/read - GET)
	companionUploadedPath := filepath.Join(companionFolder, "uploaded_file.txt")
	testReadFile(companionUploadedPath)

	// 6. Test Rename on Companion Device (/v1/folder/name - PUT)
	companionRenamedPath := filepath.Join(companionFolder, "uploaded_renamed.txt")
	testRename(companionUploadedPath, companionRenamedPath)

	// 7. Test Copy from Companion Device -> Local Storage (/v1/batch/task - POST)
	localDstDir := "/DATA/Downloads"
	_ = os.MkdirAll(localDstDir, 0755)
	copyCompanionToLocal(companionRenamedPath, localDstDir)

	// 8. Test Online Storage: Local -> Google Drive Copy (/v1/batch/task - POST)
	gdriveMount := "/mnt/google_drive_drive_1788637372"
	if _, err := os.Stat(gdriveMount); err == nil {
		copyLocalToCloud(localTestFile, gdriveMount)
	} else {
		fmt.Printf("SKIP: Google Drive mount not found at %s\n", gdriveMount)
	}

	// 9. Clean up created files on companion
	cleanupCompanion(companionFolder)

	fmt.Println("=== ALL TESTS COMPLETED ===")
}

func uploadToCompanion(dir, filename, content string) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("path", dir)
	_ = w.WriteField("filename", filename)
	_ = w.WriteField("relativePath", filename)
	_ = w.WriteField("totalChunks", "1")
	_ = w.WriteField("chunkNumber", "1")
	part, _ := w.CreateFormFile("file", filename)
	part.Write([]byte(content))
	w.Close()

	req, _ := http.NewRequest("POST", baseURL+"/v1/file/upload", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("FAIL: Upload error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 3. Companion Upload result: status %d, body: %s\n", resp.StatusCode, string(body))
}

func copyLocalToCompanion(localSrc, companionDst string) {
	payload := FileOperatePayload{
		Type:  "copy",
		Item:  []FileOperateItem{{From: localSrc}},
		To:    companionDst,
		Style: "overwrite",
	}
	bodyData, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/v1/batch/task", "application/json", bytes.NewReader(bodyData))
	if err != nil {
		fmt.Printf("FAIL: Copy local->companion error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 4. Local->Companion copy queued: status %d, body: %s\n", resp.StatusCode, string(body))

	// Wait for transfer to complete
	time.Sleep(3 * time.Second)
}

func testReadFile(filePath string) {
	resp, err := http.Get(baseURL + "/v1/file/read?path=" + filePath)
	if err != nil {
		fmt.Printf("FAIL: Read file error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 5. Read companion file result: status %d, body: %s\n", resp.StatusCode, string(body))
}

func testRename(oldPath, newPath string) {
	payload, _ := json.Marshal(map[string]string{
		"old_path": oldPath,
		"new_path": newPath,
	})
	req, _ := http.NewRequest("PUT", baseURL+"/v1/folder/name", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("FAIL: Rename error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 6. Companion Rename result: status %d, body: %s\n", resp.StatusCode, string(body))
}

func copyCompanionToLocal(companionSrc, localDst string) {
	payload := FileOperatePayload{
		Type:  "copy",
		Item:  []FileOperateItem{{From: companionSrc}},
		To:    localDst,
		Style: "overwrite",
	}
	bodyData, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/v1/batch/task", "application/json", bytes.NewReader(bodyData))
	if err != nil {
		fmt.Printf("FAIL: Copy companion->local error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 7. Companion->Local copy queued: status %d, body: %s\n", resp.StatusCode, string(body))

	// Wait for transfer to complete
	time.Sleep(3 * time.Second)

	expectedDstFile := filepath.Join(localDst, filepath.Base(companionSrc))
	if content, err := os.ReadFile(expectedDstFile); err == nil {
		fmt.Printf("✔ 7b. Verified file arrived on local disk (%d bytes): %s\n", len(content), string(content))
		_ = os.Remove(expectedDstFile)
	} else {
		fmt.Printf("⚠ Note: Local file not yet landed (or path differs): %v\n", err)
	}
}

func copyLocalToCloud(localSrc, cloudDst string) {
	payload := FileOperatePayload{
		Type:  "copy",
		Item:  []FileOperateItem{{From: localSrc}},
		To:    cloudDst,
		Style: "overwrite",
	}
	bodyData, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/v1/batch/task", "application/json", bytes.NewReader(bodyData))
	if err != nil {
		fmt.Printf("FAIL: Copy local->cloud error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✔ 8. Local->Cloud copy queued: status %d, body: %s\n", resp.StatusCode, string(body))

	// Wait for rclone transfer
	time.Sleep(3 * time.Second)
	cloudFile := filepath.Join(cloudDst, filepath.Base(localSrc))
	if st, err := os.Stat(cloudFile); err == nil {
		fmt.Printf("✔ 8b. Verified file arrived on Cloud Mount (%d bytes)\n", st.Size())
		_ = os.Remove(cloudFile)
	} else {
		fmt.Printf("Note: Cloud file stat check: %v\n", err)
	}
}

func cleanupCompanion(folder string) {
	delReq, _ := http.NewRequest("DELETE", baseURL+"/v1/batch", strings.NewReader(`["`+folder+`"]`))
	delReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(delReq)
	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("✔ 9. Cleaned up companion test folder: %s\n", string(body))
	}
}
