package metrics

// Hardware is the make and model of a host: what the machine is, as opposed to
// what it is doing right now. Nothing here changes while a box is powered on,
// so it is read once per session rather than on the sampling ticker.

import (
	"regexp"
	"strings"
)

// MemModule is one populated DIMM slot, when dmidecode can be read.
type MemModule struct {
	Slot         string `json:"slot"`
	SizeBytes    uint64 `json:"sizeBytes"`
	Type         string `json:"type"`
	SpeedMTs     int    `json:"speedMts"`
	Manufacturer string `json:"manufacturer"`
	PartNumber   string `json:"partNumber"`
}

// PhysicalDisk is one block device — the drive itself, not a filesystem on it.
type PhysicalDisk struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	SizeBytes uint64 `json:"sizeBytes"`
	// Kind is "SSD" or "HDD" where the host can tell, and empty where it
	// cannot (Windows, some virtual disks).
	Kind      string `json:"kind"`
	Transport string `json:"transport"`
}

// GPUDevice is one graphics adapter.
type GPUDevice struct {
	Name       string  `json:"name"`
	MemTotalMB float64 `json:"memTotalMb"`
	Driver     string  `json:"driver"`
}

// Hardware is one host's inventory.
type Hardware struct {
	ServerID string `json:"serverId"`
	OK       bool   `json:"ok"`
	Error    string `json:"error"`

	// Machine
	Vendor  string `json:"vendor"`
	Product string `json:"product"`

	// CPU
	CPUModel   string  `json:"cpuModel"`
	CPUSockets int     `json:"cpuSockets"`
	CPUCores   int     `json:"cpuCores"`
	CPUThreads int     `json:"cpuThreads"`
	CPUMaxMHz  float64 `json:"cpuMaxMhz"`

	// Memory
	MemTotalBytes uint64      `json:"memTotalBytes"`
	MemModules    []MemModule `json:"memModules"`

	Disks []PhysicalDisk `json:"disks"`
	GPUs  []GPUDevice    `json:"gpus"`
}

// hwScript follows the same one-exec rule as the vitals script. Every source
// that may be missing or unreadable (dmidecode without root, no lscpu on a
// minimal box, no GPU at all) degrades to an empty section rather than an
// error, and the parser treats every section as optional.
const hwScript = `
echo "#sys"; cat /sys/class/dmi/id/sys_vendor /sys/class/dmi/id/product_name 2>/dev/null
echo "#lscpu"; lscpu 2>/dev/null
echo "#cpuinfo"; grep -E '^(model name|physical id|cpu cores|processor|Hardware|Model)' /proc/cpuinfo 2>/dev/null
echo "#maxkhz"; cat /sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq 2>/dev/null
echo "#mem"; grep -E '^MemTotal:' /proc/meminfo 2>/dev/null
echo "#dmi"; dmidecode -t memory 2>/dev/null || true
echo "#lsblk"; lsblk -d -b -P -o NAME,MODEL,SIZE,ROTA,TRAN,TYPE 2>/dev/null || true
echo "#gpu"; command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi --query-gpu=name,memory.total,driver_version --format=csv,noheader,nounits 2>/dev/null || true
echo "#lspci"; lspci 2>/dev/null | grep -iE 'vga compatible|3d controller|display controller' || true
echo "#end"
`

// CollectHardware reads one host's inventory over a POSIX shell.
func CollectHardware(run Runner, serverID string) Hardware {
	h := Hardware{ServerID: serverID, MemModules: []MemModule{}, Disks: []PhysicalDisk{}, GPUs: []GPUDevice{}}

	res, err := run.Exec(serverID, hwScript)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	if strings.TrimSpace(res.Stdout) == "" {
		h.Error = strings.TrimSpace(res.Stderr)
		if h.Error == "" {
			h.Error = "no output; the host answered nothing"
		}
		return h
	}

	sec := split(res.Stdout)

	if sys := sec["sys"]; len(sys) > 0 {
		h.Vendor = strings.TrimSpace(first(sys))
		if len(sys) > 1 {
			h.Product = strings.TrimSpace(sys[1])
		}
	}

	h.CPUModel, h.CPUSockets, h.CPUCores, h.CPUThreads, h.CPUMaxMHz = cpuIdentity(sec["lscpu"], sec["cpuinfo"])
	// cpufreq reports kHz and beats lscpu's figure, which is absent in many VMs.
	if khz := atof(first(sec["maxkhz"])); khz > 0 {
		h.CPUMaxMHz = khz / 1000
	}

	h.MemTotalBytes = kvBytes(sec["mem"])["MemTotal"]
	h.MemModules = memModules(sec["dmi"])
	h.Disks = physicalDisks(sec["lsblk"])
	h.GPUs = gpuDevices(sec["gpu"], sec["lspci"])

	h.OK = true
	return h
}

// cpuIdentity prefers lscpu, which knows the topology on every architecture,
// and falls back to /proc/cpuinfo for boxes without util-linux. ARM boards
// often lack a "model name" row per core; their name hides in "Model" or
// "Hardware" instead.
func cpuIdentity(lscpu, cpuinfo []string) (model string, sockets, cores, threads int, maxMHz float64) {
	kv := map[string]string{}
	for _, line := range lscpu {
		k, v, ok := strings.Cut(line, ":")
		if ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	model = kv["Model name"]
	sockets = atoi(kv["Socket(s)"])
	threads = atoi(kv["CPU(s)"])
	cores = atoi(kv["Core(s) per socket"]) * sockets
	maxMHz = atof(strings.ReplaceAll(kv["CPU max MHz"], ",", "."))

	physical := map[string]bool{}
	coresPerSocket := 0
	procLines := 0
	for _, line := range cpuinfo {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "model name", "Model", "Hardware":
			if model == "" {
				model = v
			}
		case "physical id":
			physical[v] = true
		case "cpu cores":
			coresPerSocket = atoi(v)
		case "processor":
			procLines++
		}
	}
	if threads == 0 {
		threads = procLines
	}
	if sockets == 0 {
		sockets = len(physical)
	}
	if sockets == 0 && threads > 0 {
		sockets = 1
	}
	if cores == 0 {
		cores = coresPerSocket * sockets
	}
	if cores == 0 {
		cores = threads
	}
	return model, sockets, cores, threads, maxMHz
}

// memModules reads "Memory Device" blocks out of dmidecode -t memory. The
// section is empty on hosts where dmidecode is missing or needs root, and the
// panel simply shows less.
func memModules(lines []string) []MemModule {
	out := []MemModule{}
	var cur *MemModule
	flush := func() {
		if cur != nil && cur.SizeBytes > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "Memory Device" {
			flush()
			cur = &MemModule{}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Size":
			cur.SizeBytes = dmiSize(v)
		case "Type":
			// "RAM"/"DRAM" is what hypervisors report for virtual DIMMs;
			// it names no generation, so it says nothing worth showing.
			if v != "Unknown" && v != "Other" && v != "RAM" && v != "DRAM" {
				cur.Type = v
			}
		case "Speed":
			if cur.SpeedMTs == 0 {
				cur.SpeedMTs = firstInt(v)
			}
		case "Configured Memory Speed", "Configured Clock Speed":
			// The running speed, when the module is clocked under its rating.
			if n := firstInt(v); n > 0 {
				cur.SpeedMTs = n
			}
		case "Locator":
			cur.Slot = v
		case "Manufacturer":
			if !dmiPlaceholder(v) {
				cur.Manufacturer = v
			}
		case "Part Number":
			if !dmiPlaceholder(v) {
				cur.PartNumber = v
			}
		}
	}
	flush()
	return out
}

// firstInt reads the leading number out of strings like "3200 MT/s".
func firstInt(v string) int {
	f := strings.Fields(v)
	if len(f) == 0 {
		return 0
	}
	return atoi(f[0])
}

// dmiSize turns dmidecode's "16 GB" / "16384 MB" into bytes; empty slots say
// "No Module Installed" and become zero.
func dmiSize(v string) uint64 {
	f := strings.Fields(v)
	if len(f) < 2 {
		return 0
	}
	n := atou(f[0])
	switch strings.ToUpper(f[1]) {
	case "TB":
		return n << 40
	case "GB":
		return n << 30
	case "MB":
		return n << 20
	case "KB":
		return n << 10
	}
	return 0
}

// dmiPlaceholder recognises the strings vendors leave in unfilled DMI fields.
func dmiPlaceholder(v string) bool {
	u := strings.ToUpper(strings.TrimSpace(v))
	return u == "" || strings.Contains(u, "UNKNOWN") || strings.Contains(u, "NOT SPECIFIED") ||
		strings.Contains(u, "TO BE FILLED") || u == "NONE" || strings.Trim(u, "0") == ""
}

// lsblkPairRE matches one KEY="value" pair of lsblk -P, where a value may
// contain spaces and backslash-escaped quotes.
var lsblkPairRE = regexp.MustCompile(`([A-Z]+)="((?:[^"\\]|\\.)*)"`)

func physicalDisks(lines []string) []PhysicalDisk {
	out := []PhysicalDisk{}
	for _, line := range lines {
		kv := map[string]string{}
		for _, m := range lsblkPairRE.FindAllStringSubmatch(line, -1) {
			kv[m[1]] = strings.ReplaceAll(m[2], `\"`, `"`)
		}
		name := kv["NAME"]
		if name == "" || kv["TYPE"] != "disk" ||
			strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "sr") {
			continue
		}
		kind := ""
		switch kv["ROTA"] {
		case "1":
			kind = "HDD"
		case "0":
			kind = "SSD"
		}
		out = append(out, PhysicalDisk{
			Name:      name,
			Model:     strings.TrimSpace(kv["MODEL"]),
			SizeBytes: atou(kv["SIZE"]),
			Kind:      kind,
			Transport: kv["TRAN"],
		})
	}
	return out
}

// gpuDevices takes nvidia-smi's answer when there is one, and otherwise falls
// back to display adapters found on the PCI bus, where only the name is known.
func gpuDevices(nvidia, lspci []string) []GPUDevice {
	out := []GPUDevice{}
	for _, line := range nvidia {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 3 {
			continue
		}
		out = append(out, GPUDevice{
			Name:       strings.TrimSpace(parts[0]),
			MemTotalMB: atof(parts[1]),
			Driver:     strings.TrimSpace(parts[2]),
		})
	}
	if len(out) > 0 {
		return out
	}
	for _, line := range lspci {
		// "01:00.0 VGA compatible controller: AMD ... [Radeon RX 6800]"
		_, name, ok := strings.Cut(line, "controller:")
		if !ok {
			continue
		}
		name = strings.TrimSpace(strings.SplitN(name, "(rev", 2)[0])
		if name != "" {
			out = append(out, GPUDevice{Name: name})
		}
	}
	return out
}
