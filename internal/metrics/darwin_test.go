package metrics

import (
	"strings"
	"testing"
)

// One poll from an Apple Silicon Mac, transcribed from what each of these
// tools prints. The awkward parts are all here on purpose: iostat leads with a
// variable number of disk columns, netstat repeats an interface once per
// address and only counts bytes on its <Link> row, vm_stat names its rows in
// prose, df carries inode columns that a stripped-down df would not, and
// kern.boottime answers in a struct.
const darwinFixture = `#kv
hw.ncpu	10
hw.memsize	34359738368
hw.pagesize	16384
kern.num_files	4832
kern.maxfiles	245760
kern.boottime	{ sec = 1755000000, usec = 123456 } Tue Aug 12 09:20:00 2025
vm.loadavg	{ 2.31 2.05 1.88 }
vm.swapusage	total = 2048.00M  used = 512.25M  free = 1535.75M  (encrypted)
#now
1755086400
#os
macOS
15.5
#kernel
24.5.0
arm64
studio.local
#vmstat
Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              123456.
Pages active:                            600000.
Pages inactive:                          400000.
Pages speculative:                        50000.
Pages throttled:                              0.
Pages wired down:                        300000.
Pages purgeable:                          20000.
"Translation faults":                 987654321.
Pages copy-on-write:                   12345678.
Pages zero filled:                    234567890.
Pages reactivated:                       456789.
File-backed pages:                       250000.
Anonymous pages:                         800000.
Pages stored in compressor:              180000.
Pages occupied by compressor:            100000.
Swapins:                                      0.
Swapouts:                                     0.
#net1
Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                          8000     0    1000000     8000     0    1000000     0
lo0   16384 127           127.0.0.1           8000     0    1000000     8000     0    1000000     0
en0   1500  <Link#4>    aa:bb:cc:dd:ee:ff   500000     0  700000000   300000     0  120000000     0
en0   1500  192.168.1     192.168.1.42       500000     0  700000000   300000     0  120000000     0
#cpu
              disk0               disk4       cpu    load average
    KB/t  tps  MB/s     KB/t  tps  MB/s  us sy id   1m   5m   15m
   30.11    5  0.15    16.00    0  0.00   4  3 93 2.31 2.05 1.88
   22.40   12  0.26    16.00    0  0.00  12  8 80 2.31 2.05 1.88
#net2
Name  Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0   16384 <Link#1>                          8000     0    1000000     8000     0    1000000     0
en0   1500  <Link#4>    aa:bb:cc:dd:ee:ff   500100     0  702000000   300100     0  120500000     0
#procs
     612
#users
       3
#conns
      27
#disk
/dev/disk3s1s1 971350180 10485760 800000000     2%  500000 4000000000    0%   /
/dev/disk3s5   971350180 40000000 800000000     5%  600000 4000000000    0%   /System/Volumes/Data
devfs                209      209         0   100%     724          0  100%   /dev
map auto_home          0        0         0   100%       0          0  100%   /System/Volumes/Data/home
#mount
/dev/disk3s1s1 on / (apfs, sealed, local, read-only, journaled)
devfs on /dev (devfs, local, nobrowse)
/dev/disk3s5 on /System/Volumes/Data (apfs, local, journaled, nobrowse)
map auto_home on /System/Volumes/Data/home (autofs, automounted, nobrowse)
#topcpu
 42.1  3.2   904 tan              WindowServer
 18.7  9.9  1204 tan              Code Helper (Renderer)
#topmem
  1.2 12.4  1204 tan              Code Helper (Renderer)
  0.4  8.1  2210 tan              ollama
#end
`

func TestCollectDarwin(t *testing.T) {
	s := CollectDarwin(stubRunner{stdout: darwinFixture}, "mac", 100)
	if !s.OK {
		t.Fatalf("collect failed: %s", s.Error)
	}

	if s.Distro != "macOS 15.5" || s.Kernel != "Darwin 24.5.0" || s.Arch != "arm64" ||
		s.Hostname != "studio.local" {
		t.Errorf("identity = %q / %q / %q / %q", s.Distro, s.Kernel, s.Arch, s.Hostname)
	}

	// iostat's second line, read past however many disks it listed first.
	if s.CPUUser != 12 || s.CPUSystem != 8 || s.CPUPercent != 20 {
		t.Errorf("cpu = %v user, %v sys, %v busy", s.CPUUser, s.CPUSystem, s.CPUPercent)
	}
	if s.Cores != 10 || s.Load1 != 2.31 || s.Load15 != 1.88 {
		t.Errorf("cores = %d, load = %v %v %v", s.Cores, s.Load1, s.Load5, s.Load15)
	}
	if got, want := s.LoadPerCore, 0.231; got < want-0.001 || got > want+0.001 {
		t.Errorf("load per core = %v, want %v", got, want)
	}

	// (active + wired + compressor) * 16 KiB, with file-backed pages counted
	// as cache rather than as used.
	const page = 16384
	if want := uint64(1000000 * page); s.MemUsedBytes != want {
		t.Errorf("mem used = %d, want %d", s.MemUsedBytes, want)
	}
	if want := uint64(250000 * page); s.MemCachedBytes != want {
		t.Errorf("mem cached = %d, want %d", s.MemCachedBytes, want)
	}
	if s.MemTotalBytes != 34359738368 {
		t.Errorf("mem total = %d", s.MemTotalBytes)
	}
	if got := s.MemPercent; got < 47.6 || got > 47.7 {
		t.Errorf("mem percent = %v", got)
	}
	if s.SwapTotal != 2048*1<<20 || s.SwapUsed != uint64(512.25*(1<<20)) {
		t.Errorf("swap = %d used of %d", s.SwapUsed, s.SwapTotal)
	}

	if s.UptimeSeconds != 86400 {
		t.Errorf("uptime = %d, want 86400", s.UptimeSeconds)
	}
	if s.Processes != 612 || s.Users != 3 || s.Connections != 27 {
		t.Errorf("counts = %d procs, %d users, %d conns", s.Processes, s.Users, s.Connections)
	}
	if s.OpenFDs != 4832 || s.MaxFDs != 245760 {
		t.Errorf("fds = %d / %d", s.OpenFDs, s.MaxFDs)
	}

	// Only en0 moved, and only its <Link> row counts — the address rows repeat
	// the same counters and would double it.
	if len(s.Nets) != 1 || s.Nets[0].Name != "en0" {
		t.Fatalf("nets = %+v", s.Nets)
	}
	if s.Nets[0].RxPS != 2000000 || s.Nets[0].TxPS != 500000 {
		t.Errorf("en0 = %v rx, %v tx", s.Nets[0].RxPS, s.Nets[0].TxPS)
	}

	// The two real volumes, with devfs and the automounter left out.
	if len(s.Disks) != 2 {
		t.Fatalf("disks = %+v", s.Disks)
	}
	root := s.Disks[0]
	if root.Mount != "/" || root.Type != "apfs" || root.FS != "/dev/disk3s1s1" {
		t.Errorf("root = %+v", root)
	}
	if root.TotalBytes != 971350180*1024 || root.UsedBytes != 10485760*1024 {
		t.Errorf("root sizes = %d used of %d", root.UsedBytes, root.TotalBytes)
	}

	if len(s.TopCPU) != 2 || s.TopCPU[0].Command != "WindowServer" || s.TopCPU[0].CPU != 42.1 {
		t.Errorf("top cpu = %+v", s.TopCPU)
	}
	if len(s.TopMem) != 2 || s.TopMem[0].Mem != 12.4 || s.TopMem[0].PID != 1204 {
		t.Errorf("top mem = %+v", s.TopMem)
	}
}

// A Mac that answers less: no iostat, no system_profiler, a df without inode
// columns. Every one of those is a section that goes missing, and none of them
// may take the reading down with it.
func TestCollectDarwinDegrades(t *testing.T) {
	const thin = `#kv
hw.ncpu	4
hw.memsize	8589934592
hw.pagesize	4096
vm.loadavg	{ 0.50 0.60 0.70 }
#os
macOS
13.6
#kernel
22.6.0
x86_64
mini.local
#vmstat
#cpu
#disk
/dev/disk1s1 100000 25000 75000 25% /
#mount
/dev/disk1s1 on / (apfs, local, journaled)
#end
`
	s := CollectDarwin(stubRunner{stdout: thin}, "mac", 100)
	if !s.OK {
		t.Fatalf("a thin answer is still an answer: %s", s.Error)
	}
	if s.Cores != 4 || s.MemTotalBytes != 8589934592 {
		t.Errorf("cores = %d, mem = %d", s.Cores, s.MemTotalBytes)
	}
	// No iostat means no CPU figure, but the load averages still come from the
	// sysctl fallback.
	if s.CPUPercent != 0 || s.Load1 != 0.5 || s.Load5 != 0.6 {
		t.Errorf("cpu = %v, load = %v %v", s.CPUPercent, s.Load1, s.Load5)
	}
	if len(s.Disks) != 1 || s.Disks[0].Mount != "/" || s.Disks[0].Type != "apfs" {
		t.Fatalf("disks = %+v", s.Disks)
	}
	if s.Disks[0].UsePercent != 25 {
		t.Errorf("use percent = %v", s.Disks[0].UsePercent)
	}
	if s.Disks[0].InodePct != 0 {
		t.Errorf("a df without inode columns should report no inode figure, got %v", s.Disks[0].InodePct)
	}
}

// An Apple Silicon inventory: one chip, no DIMM slots, one internal SSD behind
// Apple Fabric, and a GPU that is part of the package.
const darwinHWFixture = `#kv
hw.model	Mac14,13
hw.memsize	34359738368
hw.physicalcpu	12
hw.logicalcpu	12
hw.packages	1
hw.cpufrequency_max
machdep.cpu.brand_string	Apple M2 Max
#sp
Hardware:

    Hardware Overview:

      Model Name: Mac Studio
      Model Identifier: Mac14,13
      Chip: Apple M2 Max
      Total Number of Cores: 12 (8 performance and 4 efficiency)
      Memory: 32 GB
      System Firmware Version: 10151.140.19

Graphics/Displays:

    Apple M2 Max:

      Chipset Model: Apple M2 Max
      Type: GPU
      Bus: Built-In
      Total Number of Cores: 30
      Metal Support: Metal 3

Memory:

    Memory:

      Memory: 32 GB
      Type: LPDDR5
      Manufacturer: Micron

#disk
   Device Identifier:         disk0
   Device Node:               /dev/disk0
   Whole:                     Yes
   Part of Whole:             disk0
   Device / Media Name:       APPLE SSD AP1024Z
   Virtual:                   No
   Protocol:                  Apple Fabric
   Solid State:               Yes
   Disk Size:                 994.7 GB (994662584320 Bytes) (exactly 1942700360 512-Byte-Units)
**********
   Device Identifier:         disk3
   Device Node:               /dev/disk3
   Whole:                     Yes
   Part of Whole:             disk3
   Device / Media Name:       APPLE SSD AP1024Z
   Virtual:                   Yes
   Protocol:                  Apple Fabric
   Solid State:               Yes
   Disk Size:                 994.7 GB (994662584320 Bytes)
**********
   Device Identifier:         disk3s1
   Whole:                     No
   Virtual:                   Yes
   Disk Size:                 11.1 GB (11072471040 Bytes)
**********
#end
`

func TestCollectDarwinHardware(t *testing.T) {
	h := CollectDarwinHardware(stubRunner{stdout: darwinHWFixture}, "mac")
	if !h.OK {
		t.Fatalf("collect failed: %s", h.Error)
	}
	if h.Vendor != "Apple" || h.Product != "Mac Studio (Mac14,13)" {
		t.Errorf("machine = %q %q", h.Vendor, h.Product)
	}
	if h.CPUModel != "Apple M2 Max" || h.CPUCores != 12 || h.CPUThreads != 12 || h.CPUSockets != 1 {
		t.Errorf("cpu = %q, %d cores, %d threads, %d sockets",
			h.CPUModel, h.CPUCores, h.CPUThreads, h.CPUSockets)
	}
	// Apple Silicon publishes no maximum frequency; an invented one would be
	// worse than none.
	if h.CPUMaxMHz != 0 {
		t.Errorf("cpu max = %v", h.CPUMaxMHz)
	}
	if h.MemTotalBytes != 34359738368 {
		t.Errorf("mem = %d", h.MemTotalBytes)
	}
	// Soldered memory has no slots to list.
	if len(h.MemModules) != 0 {
		t.Errorf("mem modules = %+v", h.MemModules)
	}
	// The container and the volume are virtual; only the device is a disk.
	if len(h.Disks) != 1 {
		t.Fatalf("disks = %+v", h.Disks)
	}
	d := h.Disks[0]
	if d.Name != "disk0" || d.Model != "APPLE SSD AP1024Z" || d.Kind != "SSD" ||
		d.Transport != "Apple Fabric" || d.SizeBytes != 994662584320 {
		t.Errorf("disk = %+v", d)
	}
	if len(h.GPUs) != 1 || h.GPUs[0].Name != "Apple M2 Max" || h.GPUs[0].Driver != "Metal 3" {
		t.Errorf("gpus = %+v", h.GPUs)
	}
}

// An Intel Mac still has DIMM slots, and system_profiler lists them.
func TestDarwinMemModules(t *testing.T) {
	const sp = `Memory:

    Memory Slots:

      ECC: Disabled
      Upgradeable Memory: Yes

      BANK 0/ChannelA-DIMM0:

          Size: 16 GB
          Type: DDR4
          Speed: 2667 MHz
          Status: OK
          Manufacturer: Samsung
          Part Number: M471A2K43DB1-CTD

      BANK 1/ChannelB-DIMM0:

          Size: 16 GB
          Type: DDR4
          Speed: 2667 MHz
          Manufacturer: Samsung
          Part Number: M471A2K43DB1-CTD
`
	mods := darwinMemModules(strings.Split(sp, "\n"))
	if len(mods) != 2 {
		t.Fatalf("modules = %+v", mods)
	}
	if mods[0].Slot != "BANK 0/ChannelA-DIMM0" || mods[0].SizeBytes != 16*(1<<30) ||
		mods[0].Type != "DDR4" || mods[0].SpeedMTs != 2667 || mods[0].Manufacturer != "Samsung" {
		t.Errorf("first module = %+v", mods[0])
	}
	if mods[1].Slot != "BANK 1/ChannelB-DIMM0" {
		t.Errorf("second module = %+v", mods[1])
	}
}
