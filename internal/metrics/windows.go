package metrics

// Vitals for hosts whose shell is PowerShell — the native Windows local host.
// No build tag: which collector to run is a property of the host being asked.

import (
	"strconv"
	"strings"
)

const winSep = "~@~"

// winScript reads everything in one PowerShell invocation, mirroring the POSIX
// collector's one-round-trip rule. Rates that need two samples around a sleep
// are skipped rather than faked; the panel shows what a Windows host can answer
// cheaply.
const winScript = `$ErrorActionPreference='SilentlyContinue'; ` +
	`$os = Get-CimInstance Win32_OperatingSystem; ` +
	`'host~@~' + $env:COMPUTERNAME; ` +
	`'distro~@~' + $os.Caption; ` +
	`'kernel~@~' + $os.Version; ` +
	`'arch~@~' + $env:PROCESSOR_ARCHITECTURE; ` +
	`'cores~@~' + [Environment]::ProcessorCount; ` +
	`'cpu~@~' + ((Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average); ` +
	`'memtotal~@~' + ($os.TotalVisibleMemorySize * 1024); ` +
	`'memfree~@~' + ($os.FreePhysicalMemory * 1024); ` +
	`'uptime~@~' + [int64]((Get-Date) - $os.LastBootUpTime).TotalSeconds; ` +
	`'procs~@~' + (Get-Process).Count; ` +
	`Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object { 'disk~@~' + $_.DeviceID + '~@~' + $_.Size + '~@~' + $_.FreeSpace }; ` +
	`Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 5 | ForEach-Object { 'topmem~@~' + $_.Id + '~@~' + $_.ProcessName + '~@~' + $_.WorkingSet64 }`

// CollectWindows reads one sample from a PowerShell host.
func CollectWindows(run Runner, serverID string, at int64) Sample {
	s := Sample{ServerID: serverID, At: at}
	res, err := run.Exec(serverID, winScript)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	if strings.TrimSpace(res.Stdout) == "" {
		s.Error = strings.TrimSpace(res.Stderr)
		if s.Error == "" {
			s.Error = "the host returned nothing"
		}
		return s
	}
	s.OK = true

	var memTotal, memFree uint64
	for _, line := range strings.Split(strings.ReplaceAll(res.Stdout, "\r\n", "\n"), "\n") {
		parts := strings.Split(strings.TrimSpace(line), winSep)
		if len(parts) < 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		switch parts[0] {
		case "host":
			s.Hostname = val
		case "distro":
			s.Distro = val
		case "kernel":
			s.Kernel = val
		case "arch":
			s.Arch = val
		case "cores":
			s.Cores, _ = strconv.Atoi(val)
		case "cpu":
			s.CPUPercent, _ = strconv.ParseFloat(val, 64)
		case "memtotal":
			memTotal, _ = strconv.ParseUint(val, 10, 64)
		case "memfree":
			memFree, _ = strconv.ParseUint(val, 10, 64)
		case "uptime":
			s.UptimeSeconds, _ = strconv.ParseInt(val, 10, 64)
		case "procs":
			s.Processes, _ = strconv.Atoi(val)
		case "disk":
			if len(parts) < 4 {
				continue
			}
			total, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
			free, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
			if total == 0 {
				continue
			}
			used := total - free
			s.Disks = append(s.Disks, Disk{
				Mount:      val,
				FS:         val,
				Type:       "NTFS",
				TotalBytes: total,
				UsedBytes:  used,
				UsePercent: float64(used) / float64(total) * 100,
			})
		case "topmem":
			if len(parts) < 4 {
				continue
			}
			pid, _ := strconv.Atoi(val)
			ws, _ := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64)
			s.TopMem = append(s.TopMem, Proc{
				PID:     pid,
				Command: strings.TrimSpace(parts[2]),
				Mem:     memPercentOf(ws, memTotal),
			})
		}
	}
	s.MemTotalBytes = memTotal
	if memTotal >= memFree {
		s.MemUsedBytes = memTotal - memFree
	}
	if memTotal > 0 {
		s.MemPercent = float64(s.MemUsedBytes) / float64(memTotal) * 100
	}
	return s
}

func memPercentOf(bytes, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(bytes) / float64(total) * 100
}

// winHwScript reads the machine's inventory in one PowerShell invocation.
const winHwScript = `$ErrorActionPreference='SilentlyContinue'; ` +
	`$cs = Get-CimInstance Win32_ComputerSystem; ` +
	`'vendor~@~' + $cs.Manufacturer; ` +
	`'product~@~' + $cs.Model; ` +
	`'memtotal~@~' + $cs.TotalPhysicalMemory; ` +
	`Get-CimInstance Win32_Processor | ForEach-Object { 'cpu~@~' + $_.Name + '~@~' + $_.NumberOfCores + '~@~' + $_.NumberOfLogicalProcessors + '~@~' + $_.MaxClockSpeed }; ` +
	`Get-CimInstance Win32_PhysicalMemory | ForEach-Object { 'mem~@~' + $_.DeviceLocator + '~@~' + $_.Capacity + '~@~' + $_.SMBIOSMemoryType + '~@~' + $_.Speed + '~@~' + $_.Manufacturer + '~@~' + $_.PartNumber }; ` +
	`Get-CimInstance Win32_DiskDrive | ForEach-Object { 'disk~@~' + $_.Model + '~@~' + $_.Size + '~@~' + $_.InterfaceType }; ` +
	`Get-CimInstance Win32_VideoController | ForEach-Object { 'gpu~@~' + $_.Name + '~@~' + $_.AdapterRAM + '~@~' + $_.DriverVersion }`

// smbiosMemoryType names the DDR generation behind Win32_PhysicalMemory's
// SMBIOS type code. Codes outside the table stay blank rather than guessed.
var smbiosMemoryType = map[int]string{
	20: "DDR", 21: "DDR2", 24: "DDR3", 26: "DDR4", 34: "DDR5", 35: "LPDDR5",
}

// CollectWindowsHardware reads one PowerShell host's inventory.
func CollectWindowsHardware(run Runner, serverID string) Hardware {
	h := Hardware{ServerID: serverID, MemModules: []MemModule{}, Disks: []PhysicalDisk{}, GPUs: []GPUDevice{}}
	res, err := run.Exec(serverID, winHwScript)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	if strings.TrimSpace(res.Stdout) == "" {
		h.Error = strings.TrimSpace(res.Stderr)
		if h.Error == "" {
			h.Error = "the host returned nothing"
		}
		return h
	}

	for _, line := range strings.Split(strings.ReplaceAll(res.Stdout, "\r\n", "\n"), "\n") {
		parts := strings.Split(strings.TrimSpace(line), winSep)
		if len(parts) < 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		switch parts[0] {
		case "vendor":
			h.Vendor = val
		case "product":
			h.Product = val
		case "memtotal":
			h.MemTotalBytes, _ = strconv.ParseUint(val, 10, 64)
		case "cpu":
			// A multi-socket board prints one row per package.
			if len(parts) < 5 {
				continue
			}
			if h.CPUModel == "" {
				h.CPUModel = val
			}
			h.CPUSockets++
			h.CPUCores += atoi(parts[2])
			h.CPUThreads += atoi(parts[3])
			if mhz := atof(parts[4]); mhz > h.CPUMaxMHz {
				h.CPUMaxMHz = mhz
			}
		case "mem":
			if len(parts) < 7 {
				continue
			}
			size, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
			if size == 0 {
				continue
			}
			h.MemModules = append(h.MemModules, MemModule{
				Slot:         val,
				SizeBytes:    size,
				Type:         smbiosMemoryType[atoi(parts[3])],
				SpeedMTs:     atoi(parts[4]),
				Manufacturer: strings.TrimSpace(parts[5]),
				PartNumber:   strings.TrimSpace(parts[6]),
			})
		case "disk":
			if len(parts) < 4 {
				continue
			}
			size, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
			if val == "" && size == 0 {
				continue
			}
			h.Disks = append(h.Disks, PhysicalDisk{
				Name:      val,
				Model:     val,
				SizeBytes: size,
				Transport: strings.TrimSpace(parts[3]),
			})
		case "gpu":
			if len(parts) < 4 || val == "" {
				continue
			}
			vram, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
			h.GPUs = append(h.GPUs, GPUDevice{
				Name:       val,
				MemTotalMB: float64(vram) / (1024 * 1024),
				Driver:     strings.TrimSpace(parts[3]),
			})
		}
	}
	h.OK = true
	return h
}
