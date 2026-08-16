// Package metrics reads host vitals over SSH.
//
// Everything comes back in a single command. The alternative — one exec per
// figure — would mean a dozen round trips every few seconds per server, which
// is exactly the kind of cost that makes a monitoring panel not worth having.
//
// Rates (CPU busy, network throughput, disk I/O) are measured inside that one
// command by sampling twice around a short sleep. That keeps the backend
// stateless and makes a reading mean the same thing no matter how often the UI
// asks for it.
package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"agentmux/internal/sshx"
)

// Runner executes a command on a server. *sshx.Pool satisfies it.
type Runner interface {
	Exec(serverID, cmd string) (sshx.ExecResult, error)
}

// Disk is one mounted filesystem.
type Disk struct {
	Mount      string  `json:"mount"`
	FS         string  `json:"fs"`
	Type       string  `json:"type"`
	TotalBytes uint64  `json:"totalBytes"`
	UsedBytes  uint64  `json:"usedBytes"`
	UsePercent float64 `json:"usePercent"`
	InodePct   float64 `json:"inodePercent"`
}

// Net is throughput on one interface, in bytes per second.
type Net struct {
	Name string  `json:"name"`
	RxPS float64 `json:"rxps"`
	TxPS float64 `json:"txps"`
}

// BlockIO is throughput on one block device, in bytes per second.
type BlockIO struct {
	Name    string  `json:"name"`
	ReadPS  float64 `json:"readps"`
	WritePS float64 `json:"writeps"`
}

// Proc is one row of the top-processes tables.
type Proc struct {
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"`
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	Command string  `json:"command"`
}

// GPU is one NVIDIA device, when nvidia-smi is present.
type GPU struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	UtilPercent float64 `json:"utilPercent"`
	MemTotalMB  float64 `json:"memTotalMb"`
	MemUsedMB   float64 `json:"memUsedMb"`
	TempC       float64 `json:"tempC"`
	PowerW      float64 `json:"powerW"`
}

// Sample is one reading of a host.
type Sample struct {
	ServerID string `json:"serverId"`
	At       int64  `json:"at"`
	OK       bool   `json:"ok"`
	Error    string `json:"error"`

	// Identity
	Distro   string `json:"distro"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`

	// CPU
	Cores        int       `json:"cores"`
	CPUPercent   float64   `json:"cpuPercent"`
	CPUUser      float64   `json:"cpuUser"`
	CPUSystem    float64   `json:"cpuSystem"`
	CPUIOWait    float64   `json:"cpuIowait"`
	CPUSteal     float64   `json:"cpuSteal"`
	PerCore      []float64 `json:"perCore"`
	Load1        float64   `json:"load1"`
	Load5        float64   `json:"load5"`
	Load15       float64   `json:"load15"`
	LoadPerCore  float64   `json:"loadPerCore"`
	ContextRate  float64   `json:"contextRate"`
	ProcsRunning int       `json:"procsRunning"`
	ProcsBlocked int       `json:"procsBlocked"`

	// Memory
	MemTotalBytes  uint64  `json:"memTotalBytes"`
	MemUsedBytes   uint64  `json:"memUsedBytes"`
	MemCachedBytes uint64  `json:"memCachedBytes"`
	MemPercent     float64 `json:"memPercent"`
	SwapTotal      uint64  `json:"swapTotalBytes"`
	SwapUsed       uint64  `json:"swapUsedBytes"`

	// Host
	UptimeSeconds int64   `json:"uptimeSeconds"`
	Processes     int     `json:"processes"`
	Users         int     `json:"users"`
	Connections   int     `json:"connections"`
	OpenFDs       int     `json:"openFds"`
	MaxFDs        int     `json:"maxFds"`
	TempC         float64 `json:"tempC"`

	Disks   []Disk    `json:"disks"`
	Nets    []Net     `json:"nets"`
	BlockIO []BlockIO `json:"blockIo"`
	TopCPU  []Proc    `json:"topCpu"`
	TopMem  []Proc    `json:"topMem"`
	GPUs    []GPU     `json:"gpus"`
}

// window is how long the remote command waits between its two samples. Long
// enough that rates are not dominated by jitter, short enough that the panel
// still feels live.
const window = 0.5

// script is deliberately one string: one exec, one round trip. Every counter
// that needs a delta is captured on both sides of the same sleep.
const script = `
S1=$(cat /proc/stat 2>/dev/null)
N1=$(cat /proc/net/dev 2>/dev/null)
D1=$(cat /proc/diskstats 2>/dev/null)
sleep 0.5
S2=$(cat /proc/stat 2>/dev/null)
N2=$(cat /proc/net/dev 2>/dev/null)
D2=$(cat /proc/diskstats 2>/dev/null)
echo "#stat1"; echo "$S1"
echo "#stat2"; echo "$S2"
echo "#net1"; echo "$N1"
echo "#net2"; echo "$N2"
echo "#dio1"; echo "$D1"
echo "#dio2"; echo "$D2"
echo "#cores"; nproc 2>/dev/null || echo 0
echo "#load"; cat /proc/loadavg 2>/dev/null
echo "#mem"; grep -E '^(MemTotal|MemFree|MemAvailable|Buffers|Cached|SReclaimable|SwapTotal|SwapFree):' /proc/meminfo 2>/dev/null
echo "#uptime"; cat /proc/uptime 2>/dev/null
echo "#procs"; ls -1d /proc/[0-9]* 2>/dev/null | wc -l
echo "#os"; (. /etc/os-release 2>/dev/null; echo "$PRETTY_NAME")
echo "#kernel"; uname -r 2>/dev/null; uname -m 2>/dev/null; hostname 2>/dev/null
echo "#users"; who 2>/dev/null | wc -l
echo "#fds"; cat /proc/sys/fs/file-nr 2>/dev/null
echo "#conns"; (ss -tan state established 2>/dev/null | tail -n +2 | wc -l) || echo 0
echo "#temp"; for f in /sys/class/thermal/thermal_zone*/temp; do [ -r "$f" ] && cat "$f"; done 2>/dev/null
echo "#disk"; df -P -k -T 2>/dev/null | tail -n +2
echo "#inode"; df -P -i 2>/dev/null | tail -n +2
echo "#topcpu"; ps -eo pcpu=,pmem=,pid=,user=,comm= --sort=-pcpu 2>/dev/null | head -5
echo "#topmem"; ps -eo pcpu=,pmem=,pid=,user=,comm= --sort=-pmem 2>/dev/null | head -5
echo "#gpu"; command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi --query-gpu=index,name,utilization.gpu,memory.total,memory.used,temperature.gpu,power.draw --format=csv,noheader,nounits 2>/dev/null || true
echo "#end"
`

// Collect reads one sample from a server.
func Collect(run Runner, serverID string, at int64) Sample {
	s := Sample{
		ServerID: serverID, At: at,
		Disks: []Disk{}, Nets: []Net{}, BlockIO: []BlockIO{},
		TopCPU: []Proc{}, TopMem: []Proc{}, GPUs: []GPU{}, PerCore: []float64{},
	}

	res, err := run.Exec(serverID, script)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	if strings.TrimSpace(res.Stdout) == "" {
		s.Error = strings.TrimSpace(res.Stderr)
		if s.Error == "" {
			s.Error = "no output; /proc is not readable on this host"
		}
		return s
	}

	sec := split(res.Stdout)

	// --- CPU, aggregate and per core ---
	before := cpuLines(sec["stat1"])
	after := cpuLines(sec["stat2"])
	if a, ok := before["cpu"]; ok {
		if b, ok2 := after["cpu"]; ok2 {
			d := b.sub(a)
			if d.total > 0 {
				s.CPUPercent = clamp(100 * float64(d.total-d.idleAll) / float64(d.total))
				s.CPUUser = clamp(100 * float64(d.user) / float64(d.total))
				s.CPUSystem = clamp(100 * float64(d.system) / float64(d.total))
				s.CPUIOWait = clamp(100 * float64(d.iowait) / float64(d.total))
				s.CPUSteal = clamp(100 * float64(d.steal) / float64(d.total))
			}
		}
	}
	for i := 0; ; i++ {
		key := "cpu" + strconv.Itoa(i)
		a, ok := before[key]
		b, ok2 := after[key]
		if !ok || !ok2 {
			break
		}
		d := b.sub(a)
		if d.total > 0 {
			s.PerCore = append(s.PerCore, clamp(100*float64(d.total-d.idleAll)/float64(d.total)))
		} else {
			s.PerCore = append(s.PerCore, 0)
		}
	}

	// Context switches per second, and the run/block queues.
	c1 := counterValue(sec["stat1"], "ctxt")
	c2 := counterValue(sec["stat2"], "ctxt")
	if c2 > c1 {
		s.ContextRate = float64(c2-c1) / window
	}
	s.ProcsRunning = int(counterValue(sec["stat2"], "procs_running"))
	s.ProcsBlocked = int(counterValue(sec["stat2"], "procs_blocked"))

	s.Cores = atoi(first(sec["cores"]))
	if s.Cores == 0 {
		s.Cores = len(s.PerCore)
	}

	if f := fields(first(sec["load"])); len(f) >= 3 {
		s.Load1, s.Load5, s.Load15 = atof(f[0]), atof(f[1]), atof(f[2])
		if s.Cores > 0 {
			s.LoadPerCore = s.Load1 / float64(s.Cores)
		}
	}

	// --- memory ---
	mem := kvBytes(sec["mem"])
	s.MemTotalBytes = mem["MemTotal"]
	s.MemCachedBytes = mem["Cached"] + mem["SReclaimable"]
	available, hasAvailable := mem["MemAvailable"]
	if !hasAvailable {
		// Kernels older than 3.14 have no MemAvailable; free+buffers+cached is
		// the usual stand-in.
		available = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	if s.MemTotalBytes > available {
		s.MemUsedBytes = s.MemTotalBytes - available
	}
	if s.MemTotalBytes > 0 {
		s.MemPercent = clamp(100 * float64(s.MemUsedBytes) / float64(s.MemTotalBytes))
	}
	s.SwapTotal = mem["SwapTotal"]
	if free, ok := mem["SwapFree"]; ok && s.SwapTotal > free {
		s.SwapUsed = s.SwapTotal - free
	}

	// --- host identity and counts ---
	if f := fields(first(sec["uptime"])); len(f) >= 1 {
		s.UptimeSeconds = int64(atof(f[0]))
	}
	s.Processes = atoi(first(sec["procs"]))
	s.Distro = strings.TrimSpace(first(sec["os"]))
	if k := sec["kernel"]; len(k) >= 1 {
		s.Kernel = strings.TrimSpace(k[0])
		if len(k) >= 2 {
			s.Arch = strings.TrimSpace(k[1])
		}
		if len(k) >= 3 {
			s.Hostname = strings.TrimSpace(k[2])
		}
	}
	s.Users = atoi(first(sec["users"]))
	s.Connections = atoi(first(sec["conns"]))
	if f := fields(first(sec["fds"])); len(f) >= 3 {
		// file-nr: allocated, free, max — "in use" is allocated minus free.
		s.OpenFDs = atoi(f[0]) - atoi(f[1])
		// Some kernels (WSL among them) report an effectively unlimited
		// file-max. Showing "1120 / 9223372036854775807" is noise, so an
		// implausible ceiling is reported as no ceiling at all.
		if m := atoi(f[2]); m > 0 && m < 1<<34 {
			s.MaxFDs = m
		}
	}
	for _, line := range sec["temp"] {
		// Thermal zones report millidegrees; the hottest zone is the one worth
		// showing, and implausible values mean the zone is not a temperature.
		if v := atof(line) / 1000; v > s.TempC && v < 150 {
			s.TempC = v
		}
	}

	s.Nets = netRates(sec["net1"], sec["net2"])
	s.BlockIO = blockRates(sec["dio1"], sec["dio2"])
	s.Disks = disks(sec["disk"], inodeUsage(sec["inode"]))
	s.TopCPU = procs(sec["topcpu"])
	s.TopMem = procs(sec["topmem"])
	s.GPUs = gpus(sec["gpu"])

	s.OK = true
	return s
}

// --- parsing ----------------------------------------------------------------

func split(out string) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "#") {
			current = strings.TrimPrefix(line, "#")
			sections[current] = []string{}
			continue
		}
		if current == "" {
			continue
		}
		sections[current] = append(sections[current], line)
	}
	return sections
}

type cpuTimes struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
	total, idleAll                                        uint64
}

func (b cpuTimes) sub(a cpuTimes) cpuTimes {
	d := cpuTimes{
		user: sat(b.user, a.user), nice: sat(b.nice, a.nice),
		system: sat(b.system, a.system), idle: sat(b.idle, a.idle),
		iowait: sat(b.iowait, a.iowait), irq: sat(b.irq, a.irq),
		softirq: sat(b.softirq, a.softirq), steal: sat(b.steal, a.steal),
	}
	d.idleAll = d.idle + d.iowait
	d.total = d.user + d.nice + d.system + d.idle + d.iowait + d.irq + d.softirq + d.steal
	return d
}

func sat(b, a uint64) uint64 {
	if b < a {
		return 0
	}
	return b - a
}

// cpuLines reads every "cpu"/"cpuN" row of /proc/stat.
func cpuLines(lines []string) map[string]cpuTimes {
	out := map[string]cpuTimes{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 8 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		var c cpuTimes
		nums := make([]uint64, 0, 8)
		for _, v := range f[1:] {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				break
			}
			nums = append(nums, n)
		}
		if len(nums) < 8 {
			continue
		}
		c.user, c.nice, c.system, c.idle = nums[0], nums[1], nums[2], nums[3]
		c.iowait, c.irq, c.softirq, c.steal = nums[4], nums[5], nums[6], nums[7]
		out[f[0]] = c
	}
	return out
}

func counterValue(lines []string, key string) uint64 {
	for _, line := range lines {
		f := fields(line)
		if len(f) >= 2 && f[0] == key {
			return atou(f[1])
		}
	}
	return 0
}

func netRates(before, after []string) []Net {
	prev := netCounters(before)
	next := netCounters(after)
	out := []Net{}
	for name, b := range next {
		a, ok := prev[name]
		if !ok {
			continue
		}
		n := Net{Name: name, RxPS: float64(sat(b.rx, a.rx)) / window, TxPS: float64(sat(b.tx, a.tx)) / window}
		// Idle interfaces are noise in a panel this small.
		if n.RxPS < 1 && n.TxPS < 1 {
			continue
		}
		out = append(out, n)
	}
	sortByRate(out)
	return out
}

func sortByRate(n []Net) {
	for i := 1; i < len(n); i++ {
		for j := i; j > 0 && n[j].RxPS+n[j].TxPS > n[j-1].RxPS+n[j-1].TxPS; j-- {
			n[j], n[j-1] = n[j-1], n[j]
		}
	}
}

type counter struct{ rx, tx uint64 }

func netCounters(lines []string) map[string]counter {
	out := map[string]counter{}
	for _, line := range lines {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" || name == "" {
			continue
		}
		f := fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[name] = counter{rx: rx, tx: tx}
	}
	return out
}

// partitionRE matches a partition rather than a whole disk. Counting both would
// double the reported I/O.
var partitionRE = regexp.MustCompile(`^(sd[a-z]+[0-9]+|vd[a-z]+[0-9]+|hd[a-z]+[0-9]+|nvme[0-9]+n[0-9]+p[0-9]+|mmcblk[0-9]+p[0-9]+)$`)

type ioCounter struct{ readSectors, writeSectors uint64 }

func blockCounters(lines []string) map[string]ioCounter {
	out := map[string]ioCounter{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 10 {
			continue
		}
		name := f[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "sr") || partitionRE.MatchString(name) {
			continue
		}
		out[name] = ioCounter{readSectors: atou(f[5]), writeSectors: atou(f[9])}
	}
	return out
}

func blockRates(before, after []string) []BlockIO {
	prev := blockCounters(before)
	next := blockCounters(after)
	out := []BlockIO{}
	for name, b := range next {
		a, ok := prev[name]
		if !ok {
			continue
		}
		// /proc/diskstats counts 512-byte sectors regardless of the device's
		// own block size.
		io := BlockIO{
			Name:    name,
			ReadPS:  float64(sat(b.readSectors, a.readSectors)) * 512 / window,
			WritePS: float64(sat(b.writeSectors, a.writeSectors)) * 512 / window,
		}
		if io.ReadPS < 1 && io.WritePS < 1 {
			continue
		}
		out = append(out, io)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ReadPS+out[j].WritePS > out[j-1].ReadPS+out[j-1].WritePS; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// inodeUsage maps a mount point to its inode use percentage.
func inodeUsage(lines []string) map[string]float64 {
	out := map[string]float64{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 6 {
			continue
		}
		mount := strings.Join(f[5:], " ")
		out[mount] = atof(strings.TrimSuffix(f[4], "%"))
	}
	return out
}

func disks(lines []string, inodes map[string]float64) []Disk {
	out := []Disk{}
	skip := map[string]bool{
		"tmpfs": true, "devtmpfs": true, "overlay": true, "squashfs": true,
		"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
		"devpts": true, "none": true, "ramfs": true, "efivarfs": true,
	}
	for _, line := range lines {
		// df -P -k -T: Filesystem Type 1024-blocks Used Available Capacity Mounted-on
		f := fields(line)
		if len(f) < 7 {
			continue
		}
		fsType := f[1]
		if skip[fsType] {
			continue
		}
		total := atou(f[2]) * 1024
		used := atou(f[3]) * 1024
		if total == 0 {
			continue
		}
		mount := strings.Join(f[6:], " ")
		out = append(out, Disk{
			FS:         f[0],
			Type:       fsType,
			Mount:      mount,
			TotalBytes: total,
			UsedBytes:  used,
			UsePercent: clamp(100 * float64(used) / float64(total)),
			InodePct:   inodes[mount],
		})
	}
	return out
}

func procs(lines []string) []Proc {
	out := []Proc{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 5 {
			continue
		}
		out = append(out, Proc{
			CPU:     atof(f[0]),
			Mem:     atof(f[1]),
			PID:     atoi(f[2]),
			User:    f[3],
			Command: strings.Join(f[4:], " "),
		})
	}
	return out
}

func gpus(lines []string) []GPU {
	out := []GPU{}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 7 {
			continue
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		out = append(out, GPU{
			Index:       atoi(parts[0]),
			Name:        parts[1],
			UtilPercent: atof(parts[2]),
			MemTotalMB:  atof(parts[3]),
			MemUsedMB:   atof(parts[4]),
			TempC:       atof(parts[5]),
			PowerW:      atof(parts[6]),
		})
	}
	return out
}

// kvBytes reads /proc/meminfo style "Key:   1234 kB" lines into bytes.
func kvBytes(lines []string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range lines {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := fields(rest)
		if len(f) == 0 {
			continue
		}
		v := atou(f[0])
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			v *= 1024
		}
		out[strings.TrimSpace(key)] = v
	}
	return out
}

func first(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func fields(s string) []string { return strings.Fields(s) }

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atou(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// FormatBytes renders a byte count the way a status line should.
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
