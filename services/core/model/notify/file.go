/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-05-26 14:21:57
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-06-02 11:14:15
 * @FilePath: /CasaOS/model/notify/file.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package notify

type File struct {
	Finished       bool   `json:"finished"`
	Cancelled      bool   `json:"cancelled"`
	ProcessedSize  int64  `json:"processed_size"`
	ProcessingPath string `json:"processing_path"`
	Status         string `json:"status"`
	TotalSize      int64  `json:"total_size"`
	Id             string `json:"id"`
	To             string `json:"to"`
	Type           string `json:"type"`
	// Speed is bytes/sec, sampled server-side over the last poll interval.
	Speed int64 `json:"speed"`
}
