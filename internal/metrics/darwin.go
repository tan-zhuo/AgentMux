package metrics

// Vitals and inventory for macOS hosts — this Mac added as a host, or one
// reached over SSH.
//
// No build tag: which collector to run is a property of the host being asked,
// the same rule the Windows one follows. macOS has no /proc, no nproc, no
// `df -T`, no `ps --sort` and no `ss`, so the POSIX collector came back with
// every field empty on a Mac — a host that looked reachable and knew nothing
// about itself. Everything here is sysctl, vm_stat, df, ps and netstat, all of
// which ship with the system and none of which need root.

import (
	"strconv"
	"strings"
)

// darwinWindow is how long the rate counters are sampled over. It is the time
// `iostat -c 2` takes to produce its second line — one second — which is also
// what separates the two netstat readings, so the network rates ride the CPU
// measurement's own wait instead of adding a sleep of their own.
const darwinWindow = 1.0

// darwinScript is one exec, like every other collector here. Every command is
// guarded: a Mac locked down enough to refuse one of them loses that section
// and nothing else.
const darwinScript = `
echo "#kv"
for k in hw.ncpu hw.memsize hw.pagesize kern.num_files kern.maxfiles kern.boottime vm.loadavg vm.swapusage; do
  printf '%s\t%s\n' "$k" "$(sysctl -n $k 2>/dev/null)"
done
echo "#now"; date +%s
echo "#os"; sw_vers -productName 2>/dev/null; sw_vers -productVersion 2>/dev/null
echo "#kernel"; uname -r 2>/dev/null; uname -m 2>/dev/null; hostname 2>/dev/null
echo "#vmstat"; vm_stat 2>/dev/null
echo "#net1"; netstat -ibn 2>/dev/null
echo "#cpu"; iostat -c 2 2>/dev/null
echo "#net2"; netstat -ibn 2>/dev/null
echo "#procs"; ps -A -o pid= 2>/dev/null | wc -l
echo "#users"; who 2>/dev/null | wc -l
echo "#conns"; netstat -an -p tcp 2>/dev/null | grep -c ESTABLISHED
echo "#disk"; df -k -P -l -i 2>/dev/null | tail -n +2
echo "#mount"; mount 2>/dev/null
echo "#topcpu"; ps -A -o pcpu=,pmem=,pid=,user=,comm= -r 2>/dev/null | head -5
echo "#topmem"; ps -A -o pcpu=,pmem=,pid=,user=,comm= -m 2>/dev/null | head -5
echo "#end"
`

// CollectDarwin reads one sample from a macOS host.
func CollectDarwin(run Runner, serverID string, at int64) Sample {
	s := Sample{
		ServerID: serverID, At: at,
		Disks: []Disk{}, Nets: []Net{}, BlockIO: []BlockIO{},
		TopCPU: []Proc{}, TopMem: []Proc{}, GPUs: []GPU{}, PerCore: []float64{},
	}

	res, err := run.Exec(serverID, darwinScript)
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
	sec := split(res.Stdout)
	kv := labelled(sec["kv"])

	// --- identity ---
	if os := sec["os"]; len(os) >= 1 {
		s.Distro = strings.TrimSpace(strings.Join(trimAll(os), " "))
	}
	if k := sec["kernel"]; len(k) >= 1 {
		s.Kernel = "Darwin " + strings.TrimSpace(k[0])
		if len(k) >= 2 {
			s.Arch = strings.TrimSpace(k[1])
		}
		if len(k) >= 3 {
			s.Hostname = strings.TrimSpace(k[2])
		}
	}

	// --- CPU and load ---
	// iostat's last line ends in six numbers: user, system, idle, then the
	// three load averages. Read from the right, because how many disks it
	// lists first depends on the machine.
	if tail := trailingNumbers(lastNonEmpty(sec["cpu"]), 6); tail != nil {
		s.CPUUser, s.CPUSystem = clamp(tail[0]), clamp(tail[1])
		s.CPUPercent = clamp(100 - tail[2])
		s.Load1, s.Load5, s.Load15 = tail[3], tail[4], tail[5]
	}
	s.Cores = atoi(kv["hw.ncpu"])
	if s.Load1 == 0 {
		// vm.loadavg reads "{ 1.62 1.72 1.80 }" — the fallback for a Mac
		// without iostat, and the only load figure on one.
		if f := numbersIn(kv["vm.loadavg"]); len(f) >= 3 {
			s.Load1, s.Load5, s.Load15 = f[0], f[1], f[2]
		}
	}
	if s.Cores > 0 {
		s.LoadPerCore = s.Load1 / float64(s.Cores)
	}

	// --- memory ---
	s.MemTotalBytes = atou(kv["hw.memsize"])
	pages, pageSize := vmStat(sec["vmstat"])
	if pageSize == 0 {
		pageSize = atou(kv["hw.pagesize"])
	}
	if pageSize > 0 {
		// What Activity Monitor calls Memory Used: the pages an application
		// holds, the ones the kernel has wired down, and what the compressor
		// is sitting on. Free and speculative pages are available, and
		// file-backed pages are the cache — the first thing given back under
		// pressure, so counting them as used would show a Mac at 95% while it
		// is idle.
		used := pages["active"] + pages["wired down"] + pages["occupied by compressor"]
		s.MemUsedBytes = used * pageSize
		s.MemCachedBytes = pages["file-backed"] * pageSize
		if s.MemTotalBytes > 0 && s.MemUsedBytes > s.MemTotalBytes {
			s.MemUsedBytes = s.MemTotalBytes
		}
	}
	if s.MemTotalBytes > 0 {
		s.MemPercent = clamp(100 * float64(s.MemUsedBytes) / float64(s.MemTotalBytes))
	}
	s.SwapTotal, s.SwapUsed = swapUsage(kv["vm.swapusage"])

	// --- host counts ---
	if boot := namedNumber(kv["kern.boottime"], "sec"); boot > 0 {
		if now := atoi(first(sec["now"])); int64(now) > boot {
			s.UptimeSeconds = int64(now) - boot
		}
	}
	s.Processes = atoi(first(sec["procs"]))
	s.Users = atoi(first(sec["users"]))
	s.Connections = atoi(first(sec["conns"]))
	s.OpenFDs = atoi(kv["kern.num_files"])
	if m := atoi(kv["kern.maxfiles"]); m > 0 && m < 1<<34 {
		s.MaxFDs = m
	}

	s.Nets = darwinNetRates(sec["net1"], sec["net2"])
	s.Disks = darwinDisks(sec["disk"], mountTypes(sec["mount"]))
	s.TopCPU = procs(sec["topcpu"])
	s.TopMem = procs(sec["topmem"])

	s.OK = true
	return s
}

// --- parsing ----------------------------------------------------------------

// labelled reads the "key<tab>value" lines the script prints for sysctl, so a
// key the kernel does not have comes back empty instead of shifting every
// value after it onto the wrong name.
func labelled(lines []string) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		k, v, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func trimAll(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func lastNonEmpty(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// trailingNumbers returns the last n numeric fields of a line, or nil when the
// line does not end in that many.
func trailingNumbers(line string, n int) []float64 {
	f := fields(line)
	if len(f) < n {
		return nil
	}
	out := make([]float64, n)
	for i, v := range f[len(f)-n:] {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		out[i] = parsed
	}
	return out
}

// numbersIn pulls every number out of a line, for the sysctls that answer in
// prose: "{ 1.62 1.72 1.80 }".
func numbersIn(s string) []float64 {
	out := []float64{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return r != '.' && (r < '0' || r > '9')
	}) {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// namedNumber reads "name = value" out of a sysctl that answers in a struct:
// kern.boottime is "{ sec = 1755000000, usec = 12345 } Tue Aug 12 …".
func namedNumber(s, name string) int64 {
	at := strings.Index(s, name+" =")
	if at < 0 {
		return 0
	}
	rest := s[at+len(name)+2:]
	digits := strings.TrimLeft(rest, " ")
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	v, _ := strconv.ParseInt(digits[:end], 10, 64)
	return v
}

// vmStat reads `vm_stat` into page counts, keyed by the name between "Pages"
// and the colon ("free", "active", "wired down", "occupied by compressor"),
// along with the page size from its header.
func vmStat(lines []string) (map[string]uint64, uint64) {
	out := map[string]uint64{}
	var pageSize uint64
	for _, line := range lines {
		if strings.Contains(line, "page size of") {
			if n := numbersIn(line); len(n) > 0 {
				pageSize = uint64(n[len(n)-1])
			}
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		// "Pages free", "Pages wired down", "File-backed pages".
		switch {
		case strings.HasPrefix(name, "Pages "):
			name = strings.TrimPrefix(name, "Pages ")
		case strings.HasSuffix(name, " pages"):
			name = strings.TrimSuffix(name, " pages")
		default:
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if v, err := strconv.ParseUint(value, 10, 64); err == nil {
			out[strings.ToLower(name)] = v
		}
	}
	return out, pageSize
}

// swapUsage reads `sysctl -n vm.swapusage`:
// "total = 2048.00M  used = 512.25M  free = 1535.75M  (encrypted)".
func swapUsage(s string) (total, used uint64) {
	return swapField(s, "total"), swapField(s, "used")
}

func swapField(s, name string) uint64 {
	at := strings.Index(s, name+" =")
	if at < 0 {
		return 0
	}
	f := fields(s[at+len(name)+2:])
	if len(f) == 0 {
		return 0
	}
	return sizeSuffix(f[0])
}

// sizeSuffix parses "512.25M" into bytes. macOS writes swap in whichever unit
// keeps the number small.
func sizeSuffix(s string) uint64 {
	if s == "" {
		return 0
	}
	mult := float64(1)
	switch s[len(s)-1] {
	case 'K', 'k':
		mult = 1 << 10
	case 'M', 'm':
		mult = 1 << 20
	case 'G', 'g':
		mult = 1 << 30
	case 'T', 't':
		mult = 1 << 40
	}
	if mult > 1 {
		s = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return uint64(v * mult)
}

// mountTypes maps a mount point to its filesystem type, from `mount`:
// "/dev/disk3s1s1 on / (apfs, sealed, local, read-only, journaled)".
func mountTypes(lines []string) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		at := strings.Index(line, " on ")
		open := strings.LastIndex(line, " (")
		if at < 0 || open < at {
			continue
		}
		mount := line[at+4 : open]
		opts := strings.TrimSuffix(line[open+2:], ")")
		fsType, _, _ := strings.Cut(opts, ",")
		out[mount] = strings.TrimSpace(fsType)
	}
	return out
}

// darwinDisks reads `df -k -P -l -i`:
// Filesystem 1024-blocks Used Available Capacity iused ifree %iused Mounted-on
//
// The inode columns are there on macOS and absent on a stripped-down df, so a
// six-column line is read as a plain df and loses only its inode figure.
func darwinDisks(lines []string, types map[string]string) []Disk {
	out := []Disk{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 6 {
			continue
		}
		total := atou(f[1]) * 1024
		used := atou(f[2]) * 1024
		if total == 0 {
			continue
		}
		mountAt, inodePct := 5, float64(0)
		if len(f) >= 9 {
			mountAt = 8
			inodePct = atof(strings.TrimSuffix(f[7], "%"))
		}
		mount := strings.Join(f[mountAt:], " ")
		fsType := types[mount]
		// A Mac mounts a dozen synthetic volumes — devfs, the read-only system
		// snapshot's siblings, every app's disk image. Only what has a size
		// and is not a pseudo-filesystem is a disk anyone wants to see.
		if fsType == "devfs" || fsType == "autofs" {
			continue
		}
		out = append(out, Disk{
			FS:         f[0],
			Type:       fsType,
			Mount:      mount,
			TotalBytes: total,
			UsedBytes:  used,
			UsePercent: clamp(100 * float64(used) / float64(total)),
			InodePct:   inodePct,
		})
	}
	return out
}

// darwinNetRates turns two `netstat -ibn` readings into per-interface rates.
//
// Only the "<Link#n>" row of an interface carries its byte counters; the rows
// for each configured address repeat them and would double every figure. The
// columns after the link are Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll, with
// a Drop column appended on newer releases — so the counters are read from the
// front of that trailing run, which is where both layouts agree.
func darwinNetRates(before, after []string) []Net {
	b := darwinNetCounters(before)
	a := darwinNetCounters(after)
	out := []Net{}
	for name, now := range a {
		then, ok := b[name]
		if !ok {
			continue
		}
		rx := float64(sat(now.rx, then.rx)) / darwinWindow
		tx := float64(sat(now.tx, then.tx)) / darwinWindow
		if rx == 0 && tx == 0 {
			continue
		}
		out = append(out, Net{Name: name, RxPS: rx, TxPS: tx})
	}
	sortByRate(out)
	return out
}

func darwinNetCounters(lines []string) map[string]counter {
	out := map[string]counter{}
	for _, line := range lines {
		f := fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[2], "<Link") {
			continue
		}
		nums := f[3:]
		// The MAC address column is present on hardware interfaces and absent
		// on lo0 and friends; either way the counters are the trailing run of
		// plain integers.
		for len(nums) > 0 && !isDigits(nums[0]) {
			nums = nums[1:]
		}
		if len(nums) < 6 {
			continue
		}
		out[strings.TrimSuffix(f[0], "*")] = counter{rx: atou(nums[2]), tx: atou(nums[5])}
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// --- inventory --------------------------------------------------------------

// darwinHWScript reads what the machine is, rather than what it is doing. Read
// once per session, so it can afford system_profiler — which takes a second or
// two and is the only place a Mac names its own model and its GPU.
const darwinHWScript = `
echo "#kv"
for k in hw.model hw.memsize hw.physicalcpu hw.logicalcpu hw.packages hw.cpufrequency_max machdep.cpu.brand_string; do
  printf '%s\t%s\n' "$k" "$(sysctl -n $k 2>/dev/null)"
done
echo "#sp"; system_profiler SPHardwareDataType SPDisplaysDataType SPMemoryDataType 2>/dev/null
echo "#disk"; diskutil info -all 2>/dev/null
echo "#end"
`

// CollectDarwinHardware reads one macOS host's inventory.
func CollectDarwinHardware(run Runner, serverID string) Hardware {
	h := Hardware{ServerID: serverID, MemModules: []MemModule{}, Disks: []PhysicalDisk{}, GPUs: []GPUDevice{}}

	res, err := run.Exec(serverID, darwinHWScript)
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
	sec := split(res.Stdout)
	kv := labelled(sec["kv"])
	sp := profilerFields(sec["sp"])

	h.Vendor = "Apple"
	// "MacBook Pro" reads better than "Mac14,6", but the identifier is the one
	// thing every Mac can answer without system_profiler.
	h.Product = firstNonEmpty(sp["Model Name"], kv["hw.model"])
	if name, id := sp["Model Name"], kv["hw.model"]; name != "" && id != "" && name != id {
		h.Product = name + " (" + id + ")"
	}

	// Apple Silicon calls it the chip and Intel calls it the processor; the
	// sysctl answers on both, and is what a Mac reached over SSH may be left
	// with if system_profiler is slow to speak.
	h.CPUModel = firstNonEmpty(sp["Chip"], sp["Processor Name"], kv["machdep.cpu.brand_string"])
	h.CPUCores = atoi(kv["hw.physicalcpu"])
	h.CPUThreads = atoi(kv["hw.logicalcpu"])
	h.CPUSockets = atoi(kv["hw.packages"])
	if h.CPUSockets == 0 && h.CPUCores > 0 {
		h.CPUSockets = 1
	}
	// Apple Silicon reports no fixed maximum; Intel Macs report it in hertz.
	h.CPUMaxMHz = atof(kv["hw.cpufrequency_max"]) / 1e6

	h.MemTotalBytes = atou(kv["hw.memsize"])
	h.MemModules = darwinMemModules(sec["sp"])
	h.Disks = darwinDisksPhysical(sec["disk"])
	h.GPUs = darwinGPUs(sec["sp"])

	h.OK = true
	return h
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// profilerFields flattens system_profiler's indented "Key: Value" report. Keys
// repeat between sections — every display has a "Chipset Model" — so the first
// occurrence wins, which is the one under Hardware Overview.
func profilerFields(lines []string) map[string]string {
	out := map[string]string{}
	for _, line := range lines {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		if _, seen := out[k]; !seen {
			out[k] = v
		}
	}
	return out
}

// darwinGPUs reads the display adapters out of system_profiler's report. On
// Apple Silicon there is one, named for the chip, and its core count stands in
// for the VRAM a discrete card would report — shared memory has no separate
// total to give.
func darwinGPUs(lines []string) []GPUDevice {
	out := []GPUDevice{}
	var cur *GPUDevice
	for _, line := range lines {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Chipset Model":
			out = append(out, GPUDevice{Name: v})
			cur = &out[len(out)-1]
		case "VRAM (Total)", "VRAM (Dynamic, Max)":
			if cur != nil {
				cur.MemTotalMB = vramMB(v)
			}
		case "Metal Support", "Metal":
			if cur != nil && cur.Driver == "" {
				cur.Driver = v
			}
		}
	}
	return out
}

// vramMB parses "8 GB" or "1536 MB".
func vramMB(v string) float64 {
	f := fields(v)
	if len(f) == 0 {
		return 0
	}
	n := atof(f[0])
	if len(f) > 1 && strings.EqualFold(f[1], "GB") {
		n *= 1024
	}
	return n
}

// darwinMemModules reads the DIMM blocks system_profiler prints on a Mac that
// has slots. Apple Silicon has none — its memory is part of the package — and
// comes back empty, which leaves the panel showing the total alone.
func darwinMemModules(lines []string) []MemModule {
	out := []MemModule{}
	slot := ""
	var cur *MemModule
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// A slot header is a line that is only a name and a colon:
		// "BANK 0/ChannelA-DIMM0:".
		if strings.HasSuffix(trimmed, ":") && !strings.Contains(strings.TrimSuffix(trimmed, ":"), ":") {
			slot = strings.TrimSuffix(trimmed, ":")
			cur = nil
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Size":
			size := sizeWords(v)
			if size == 0 || slot == "" {
				continue
			}
			out = append(out, MemModule{Slot: slot, SizeBytes: size})
			cur = &out[len(out)-1]
		case "Type":
			if cur != nil {
				cur.Type = v
			}
		case "Speed":
			if cur != nil {
				cur.SpeedMTs = firstInt(v)
			}
		case "Manufacturer":
			if cur != nil {
				cur.Manufacturer = v
			}
		case "Part Number":
			if cur != nil {
				cur.PartNumber = v
			}
		}
	}
	return out
}

// sizeWords parses system_profiler's "8 GB" into bytes.
func sizeWords(v string) uint64 {
	f := fields(v)
	if len(f) < 2 {
		return 0
	}
	n := atof(f[0])
	switch strings.ToUpper(f[1]) {
	case "TB":
		n *= 1 << 40
	case "GB":
		n *= 1 << 30
	case "MB":
		n *= 1 << 20
	case "KB":
		n *= 1 << 10
	default:
		return 0
	}
	return uint64(n)
}

// darwinDisksPhysical reads the whole, non-virtual devices out of
// `diskutil info -all`, which prints one block of "Key: Value" per device
// separated by a rule of asterisks. Containers and volumes are virtual and are
// skipped, or a Mac would list the same SSD five times.
func darwinDisksPhysical(lines []string) []PhysicalDisk {
	out := []PhysicalDisk{}
	block := map[string]string{}
	flush := func() {
		defer func() { block = map[string]string{} }()
		if block["Whole"] != "Yes" || block["Virtual"] == "Yes" {
			return
		}
		name := block["Device Identifier"]
		size := bytesInParens(block["Disk Size"])
		if name == "" || size == 0 {
			return
		}
		kind := ""
		switch block["Solid State"] {
		case "Yes":
			kind = "SSD"
		case "No":
			kind = "HDD"
		}
		out = append(out, PhysicalDisk{
			Name:      name,
			Model:     firstNonEmpty(block["Device / Media Name"], block["Media Name"]),
			SizeBytes: size,
			Kind:      kind,
			Transport: block["Protocol"],
		})
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "****") {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		block[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	flush()
	return out
}

// bytesInParens reads the exact figure out of "994.7 GB (994662584320 Bytes)",
// which is the only one worth keeping — the rounded one in front is decimal GB
// and would disagree with every other size in the panel.
func bytesInParens(v string) uint64 {
	open := strings.Index(v, "(")
	if open < 0 {
		return 0
	}
	f := fields(v[open+1:])
	if len(f) == 0 {
		return 0
	}
	return atou(strings.ReplaceAll(f[0], ",", ""))
}
