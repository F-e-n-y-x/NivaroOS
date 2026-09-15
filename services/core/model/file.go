/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-05-20 16:27:12
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-06-09 18:18:46
 * @FilePath: /CasaOS/model/file.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package model

type FileOperate struct {
	Type          string     `json:"type" binding:"required"`
	Item          []FileItem `json:"item" binding:"required"`
	TotalSize     int64      `json:"total_size"`
	ProcessedSize int64      `json:"processed_size"`
	To            string     `json:"to" binding:"required"`
	Style         string     `json:"style"`
	Finished      bool       `json:"finished"`
	// Cancelled is set once a user-initiated cancel (DELETE .../task/:id)
	// actually stopped this task's in-flight copy, as opposed to it
	// finishing normally - lets the UI show "Cancelled" instead of "Done".
	Cancelled bool `json:"cancelled"`
	// Speed is bytes/sec, sampled by CheckFileStatus() from the delta between
	// consecutive polls - internal only (never bound from a client request,
	// never marshalled back out of this struct directly; SendFileOperateNotify
	// copies it into notify.File.Speed for the broadcast to the UI).
	Speed int64 `json:"-"`
}

type FileItem struct {
	From          string `json:"from" binding:"required"`
	Finished      bool   `json:"finished"`
	Size          int64  `json:"size"`
	ProcessedSize int64  `json:"processed_size"`
}

type FileUpdate struct {
	FilePath    string `json:"path" binding:"required"`
	FileContent string `json:"content" binding:"required"`
}
