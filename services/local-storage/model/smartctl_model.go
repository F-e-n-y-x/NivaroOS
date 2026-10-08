package model

type SmartctlA struct {
	Smartctl struct {
		Version      []int    `json:"version"`
		SvnRevision  string   `json:"svn_revision"`
		PlatformInfo string   `json:"platform_info"`
		BuildInfo    string   `json:"build_info"`
		Argv         []string `json:"argv"`
		ExitStatus   int      `json:"exit_status"`
		Messages     []struct {
			String   string `json:"string"`
			Severity string `json:"severity"`
		} `json:"messages"`
	} `json:"smartctl"`
	Device struct {
		Name     string `json:"name"`
		InfoName string `json:"info_name"`
		Type     string `json:"type"`
		Protocol string `json:"protocol"`
	} `json:"device"`
	ModelName       string `json:"model_name"`
	SerialNumber    string `json:"serial_number"`
	FirmwareVersion string `json:"firmware_version"`
	UserCapacity    struct {
		Blocks int   `json:"blocks"`
		Bytes  int64 `json:"bytes"`
	} `json:"user_capacity"`
	SmartStatus struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	AtaSmartData struct {
		OfflineDataCollection struct {
			Status struct {
				Value  int    `json:"value"`
				String string `json:"string"`
			} `json:"status"`
			CompletionSeconds int `json:"completion_seconds"`
		} `json:"offline_data_collection"`
		SelfTest struct {
			Status struct {
				Value  int    `json:"value"`
				String string `json:"string"`
				Passed bool   `json:"passed"`
				// percent still to do while a test runs (status 241-249)
				RemainingPercent int `json:"remaining_percent"`
			} `json:"status"`
			PollingMinutes struct {
				Short      int `json:"short"`
				Extended   int `json:"extended"`
				Conveyance int `json:"conveyance"`
			} `json:"polling_minutes"`
		} `json:"self_test"`
		Capabilities struct {
			Values                        []int `json:"values"`
			ExecOfflineImmediateSupported bool  `json:"exec_offline_immediate_supported"`
			OfflineIsAbortedUponNewCmd    bool  `json:"offline_is_aborted_upon_new_cmd"`
			OfflineSurfaceScanSupported   bool  `json:"offline_surface_scan_supported"`
			SelfTestsSupported            bool  `json:"self_tests_supported"`
			ConveyanceSelfTestSupported   bool  `json:"conveyance_self_test_supported"`
			SelectiveSelfTestSupported    bool  `json:"selective_self_test_supported"`
			AttributeAutosaveEnabled      bool  `json:"attribute_autosave_enabled"`
			ErrorLoggingSupported         bool  `json:"error_logging_supported"`
			GpLoggingSupported            bool  `json:"gp_logging_supported"`
		} `json:"capabilities"`
	} `json:"ata_smart_data"`
	PowerOnTime struct {
		Hours int `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount int `json:"power_cycle_count"`
	Temperature     struct {
		Current     int `json:"current"`
		LifetimeMax int `json:"lifetime_max"`
		OpLimitMax  int `json:"op_limit_max"`
	} `json:"temperature"`

	// What the health report (smart_health.go) reads. smartctl --json -a
	// -l devstat, smartmontools 7+.
	RotationRate       int `json:"rotation_rate"` // 0: solid state; absent on NVMe
	AtaSmartAttributes struct {
		Table []SmartAttribute `json:"table"`
	} `json:"ata_smart_attributes"`
	AtaDeviceStatistics struct {
		Pages []struct {
			Name  string `json:"name"`
			Table []struct {
				Name  string `json:"name"`
				Value *int64 `json:"value"`
			} `json:"table"`
		} `json:"pages"`
	} `json:"ata_device_statistics"`
	AtaSmartErrorLog struct {
		Summary struct {
			Count int `json:"count"`
			Table []struct {
				LifetimeHours int `json:"lifetime_hours"`
			} `json:"table"`
		} `json:"summary"`
	} `json:"ata_smart_error_log"`
	AtaSmartSelfTestLog struct {
		Standard struct {
			Table []struct {
				Type struct {
					String string `json:"string"`
				} `json:"type"`
				Status struct {
					Value  int    `json:"value"`
					String string `json:"string"`
					Passed *bool  `json:"passed"`
				} `json:"status"`
				LifetimeHours int `json:"lifetime_hours"`
			} `json:"table"`
		} `json:"standard"`
	} `json:"ata_smart_self_test_log"`
	NvmeHealth *struct {
		CriticalWarning         int   `json:"critical_warning"`
		Temperature             int   `json:"temperature"`
		AvailableSpare          int   `json:"available_spare"`
		AvailableSpareThreshold int   `json:"available_spare_threshold"`
		PercentageUsed          int   `json:"percentage_used"`
		DataUnitsWritten        int64 `json:"data_units_written"`
		PowerCycles             int64 `json:"power_cycles"`
		PowerOnHours            int64 `json:"power_on_hours"`
		UnsafeShutdowns         int64 `json:"unsafe_shutdowns"`
		MediaErrors             int64 `json:"media_errors"`
		NumErrLogEntries        int64 `json:"num_err_log_entries"`
	} `json:"nvme_smart_health_information_log,omitempty"`
	NvmeSelfTestLog *struct {
		CurrentSelfTestOperation struct {
			Value  int    `json:"value"`
			String string `json:"string"`
		} `json:"current_self_test_operation"`
		CurrentSelfTestCompletionPercent int `json:"current_self_test_completion_percent"`
		Table                            []struct {
			SelfTestCode struct {
				String string `json:"string"`
			} `json:"self_test_code"`
			SelfTestResult struct {
				Value  int    `json:"value"`
				String string `json:"string"`
			} `json:"self_test_result"`
			PowerOnHours int `json:"power_on_hours"`
		} `json:"table"`
	} `json:"nvme_self_test_log,omitempty"`

	// Set by local-storage, not smartctl: the drive was asleep (smartctl -n
	// standby skipped it). When StaleHealth is true the SMART fields are the
	// last reading taken while it was awake.
	Sleeping    bool `json:"sleeping,omitempty"`
	StaleHealth bool `json:"-"`
}

type SmartAttribute struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Value      int    `json:"value"`
	Worst      int    `json:"worst"`
	Thresh     int    `json:"thresh"`
	WhenFailed string `json:"when_failed"`
	Raw        struct {
		Value  int64  `json:"value"`
		String string `json:"string"`
	} `json:"raw"`
}
