//go:build !darwin
// +build !darwin

/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-07-01 15:11:36
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-09-05 16:28:46
 * @FilePath: /CasaOS/route/periodical.go
 * @Description:
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package route

import (
	"strings"
	"time"
	"unsafe"

	"github.com/F-e-n-y-x/NivaroOS/services/core/model"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
)

// The parts of a reading that cost a shell (which NICs are physical, their
// link state) or a /proc/cpuinfo parse (the CPU vendor), read again at most
// every 5 s so 500 ms sampling stays cheap. Only the publisher goroutine
// touches these.
var (
	slowFactsAt   time.Time
	slowNets      []string
	slowNetStates map[string]string
	slowCPUModel  string
)

func refreshSlowFacts(now time.Time) {
	if !slowFactsAt.IsZero() && now.Sub(slowFactsAt) < utilizationEvery-utilizationFastEvery/2 {
		return
	}
	slowFactsAt = now
	slowNets = service.MyService.System().GetNet(true)
	slowNetStates = make(map[string]string, len(slowNets))
	for _, name := range slowNets {
		slowNetStates[name] = strings.TrimSpace(service.MyService.System().GetNetState(name))
	}
	slowCPUModel = "arm"
	if cpu := service.MyService.System().GetCpuInfo(); len(cpu) > 0 {
		if strings.Count(strings.ToLower(strings.TrimSpace(cpu[0].ModelName)), "intel") > 0 {
			slowCPUModel = "intel"
		} else if strings.Count(strings.ToLower(strings.TrimSpace(cpu[0].ModelName)), "amd") > 0 {
			slowCPUModel = "amd"
		}
	}
}

// SendAllHardwareStatusBySocket publishes one reading as event [name].
func SendAllHardwareStatusBySocket(name string) {
	now := time.Now()
	refreshSlowFacts(now)
	netList := service.MyService.System().GetNetInfo()
	newNet := []model.IOCountersStat{}
	for _, n := range netList {
		for _, netCardName := range slowNets {
			if n.Name == netCardName {
				item := *(*model.IOCountersStat)(unsafe.Pointer(&n))
				item.State = slowNetStates[n.Name]
				item.Time = now.Unix()
				newNet = append(newNet, item)
				break
			}
		}
	}
	cpu := service.MyService.System().GetCpuPercent()
	cpuModel := slowCPUModel

	num := service.MyService.System().GetCpuCoreNum()
	cpuData := make(map[string]interface{})
	cpuData["percent"] = cpu
	cpuData["percpu"] = service.MyService.System().GetCpuPercentPerCore()
	cpuData["num"] = num
	cpuData["temperature"] = service.MyService.System().GetCPUTemperature()
	cpuData["power"] = service.MyService.System().GetCPUPower()
	cpuData["model"] = cpuModel

	memInfo := service.MyService.System().GetMemInfo()

	body := make(map[string]interface{})

	body["sys_mem"] = memInfo

	body["sys_cpu"] = cpuData

	body["sys_net"] = newNet
	systemTempMap := service.MyService.Notify().GetSystemTempMap()
	systemTempMap.Range(func(key, value interface{}) bool {
		body[key.(string)] = value
		return true
	})
	service.MyService.Notify().SendNotify(name, body)
}

// func MonitoryUSB() {
// 	var matcher netlink.Matcher

// 	conn := new(netlink.UEventConn)
// 	if err := conn.Connect(netlink.UdevEvent); err != nil {
// 		logger.Error("udev err", zap.Any("Unable to connect to Netlink Kobject UEvent socket", err))
// 	}
// 	defer conn.Close()

// 	queue := make(chan netlink.UEvent)
// 	errors := make(chan error)
// 	quit := conn.Monitor(queue, errors, matcher)

// 	signals := make(chan os.Signal, 1)
// 	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
// 	go func() {
// 		<-signals
// 		close(quit)
// 		os.Exit(0)
// 	}()

// 	for {
// 		select {
// 		case uevent := <-queue:
// 			if uevent.Env["DEVTYPE"] == "disk" {
// 				time.Sleep(time.Microsecond * 500)
// 				SendUSBBySocket()
// 				continue
// 			}
// 		case err := <-errors:
// 			logger.Error("udev err", zap.Any("err", err))
// 		}
// 	}

// }
