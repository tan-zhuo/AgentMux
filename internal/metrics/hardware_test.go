package metrics

import (
	"testing"

	"agentmux/internal/sshx"
)

// stubRunner answers every exec with a canned transcript.
type stubRunner struct{ stdout string }

func (s stubRunner) Exec(string, string) (sshx.ExecResult, error) {
	return sshx.ExecResult{Stdout: s.stdout}, nil
}

// A physical box: two-socket Xeon, four DIMMs of which two slots are empty,
// one NVMe drive and one spinner, an NVIDIA card. MODEL carries spaces, which
// is why lsblk is asked for -P pairs.
const hwFixture = `#sys
Dell Inc.
PowerEdge R740
#lscpu
CPU(s):                          56
Model name:                      Intel(R) Xeon(R) Gold 6132 CPU @ 2.60GHz
Core(s) per socket:              14
Socket(s):                       2
CPU max MHz:                     3700.0000
#cpuinfo
#maxkhz
#mem
MemTotal:       131717968 kB
#dmi
Memory Device
	Size: 16 GB
	Locator: DIMM_A1
	Type: DDR4
	Speed: 2933 MT/s
	Configured Memory Speed: 2666 MT/s
	Manufacturer: Samsung
	Part Number: M393A2K43CB2-CTD
Memory Device
	Size: No Module Installed
	Locator: DIMM_A2
Memory Device
	Size: 16 GB
	Locator: DIMM_B1
	Type: DDR4
	Speed: 2933 MT/s
	Configured Memory Speed: 2666 MT/s
	Manufacturer: Samsung
	Part Number: M393A2K43CB2-CTD
#lsblk
NAME="nvme0n1" MODEL="Samsung SSD 980 PRO 1TB" SIZE="1000204886016" ROTA="0" TRAN="nvme" TYPE="disk"
NAME="sda" MODEL="ST4000NM0035-1V4" SIZE="4000787030016" ROTA="1" TRAN="sata" TYPE="disk"
NAME="sr0" MODEL="QEMU DVD-ROM" SIZE="1073741312" ROTA="1" TRAN="ata" TYPE="rom"
NAME="loop0" MODEL="" SIZE="4096" ROTA="0" TRAN="" TYPE="loop"
#gpu
NVIDIA GeForce RTX 4090, 24564, 550.54.14
#lspci
01:00.0 VGA compatible controller: NVIDIA Corporation AD102 (rev a1)
#end
`

func TestCollectHardware(t *testing.T) {
	h := CollectHardware(stubRunner{stdout: hwFixture}, "srv1")
	if !h.OK {
		t.Fatalf("not ok: %s", h.Error)
	}
	if h.Vendor != "Dell Inc." || h.Product != "PowerEdge R740" {
		t.Errorf("machine = %q %q", h.Vendor, h.Product)
	}
	if h.CPUModel != "Intel(R) Xeon(R) Gold 6132 CPU @ 2.60GHz" {
		t.Errorf("cpu model = %q", h.CPUModel)
	}
	if h.CPUSockets != 2 || h.CPUCores != 28 || h.CPUThreads != 56 {
		t.Errorf("topology = %d sockets %d cores %d threads", h.CPUSockets, h.CPUCores, h.CPUThreads)
	}
	if h.CPUMaxMHz != 3700 {
		t.Errorf("max mhz = %v", h.CPUMaxMHz)
	}
	if h.MemTotalBytes != 131717968*1024 {
		t.Errorf("mem total = %d", h.MemTotalBytes)
	}
	if len(h.MemModules) != 2 {
		t.Fatalf("modules = %d, want 2 (empty slot must not count)", len(h.MemModules))
	}
	m := h.MemModules[0]
	if m.SizeBytes != 16<<30 || m.Type != "DDR4" || m.SpeedMTs != 2666 || m.PartNumber != "M393A2K43CB2-CTD" {
		t.Errorf("module = %+v", m)
	}
	if len(h.Disks) != 2 {
		t.Fatalf("disks = %+v, want the rom and loop devices dropped", h.Disks)
	}
	if d := h.Disks[0]; d.Model != "Samsung SSD 980 PRO 1TB" || d.Kind != "SSD" || d.Transport != "nvme" {
		t.Errorf("disk = %+v", d)
	}
	if d := h.Disks[1]; d.Kind != "HDD" {
		t.Errorf("spinner = %+v", d)
	}
	if len(h.GPUs) != 1 || h.GPUs[0].Name != "NVIDIA GeForce RTX 4090" || h.GPUs[0].MemTotalMB != 24564 {
		t.Errorf("gpus = %+v (lspci must not add to an nvidia-smi answer)", h.GPUs)
	}
}

// A VM without nvidia-smi, without cpufreq, with a virtual DIMM whose type
// row says only "RAM": the fallbacks must fill in what they can and stay
// quiet about the rest.
const hwVMFixture = `#sys
Red Hat
KVM
#lscpu
CPU(s):                                  8
Model name:                              AMD EPYC 7C13 64-Core Processor
Core(s) per socket:                      4
Socket(s):                               2
#cpuinfo
#maxkhz
#mem
MemTotal:       16328780 kB
#dmi
Memory Device
	Size: 16 GB
	Locator: DIMM 0
	Type: RAM
	Speed: Unknown
	Manufacturer: Red Hat
	Part Number: Not Specified
	Configured Memory Speed: Unknown
#lsblk
NAME="vda" MODEL="" SIZE="214748364800" ROTA="1" TRAN="virtio" TYPE="disk"
#gpu
#lspci
00:02.0 VGA compatible controller: Red Hat, Inc. Virtio 1.0 GPU (rev 01)
#end
`

func TestCollectHardwareVM(t *testing.T) {
	h := CollectHardware(stubRunner{stdout: hwVMFixture}, "vm1")
	if !h.OK {
		t.Fatalf("not ok: %s", h.Error)
	}
	if h.CPUModel != "AMD EPYC 7C13 64-Core Processor" || h.CPUMaxMHz != 0 {
		t.Errorf("cpu = %q max %v", h.CPUModel, h.CPUMaxMHz)
	}
	if len(h.MemModules) != 1 {
		t.Fatalf("modules = %+v", h.MemModules)
	}
	m := h.MemModules[0]
	if m.Type != "" || m.SpeedMTs != 0 || m.PartNumber != "" || m.Manufacturer != "Red Hat" {
		t.Errorf("virtual dimm placeholders survived: %+v", m)
	}
	if len(h.Disks) != 1 || h.Disks[0].Name != "vda" || h.Disks[0].Transport != "virtio" {
		t.Errorf("disks = %+v", h.Disks)
	}
	if len(h.GPUs) != 1 || h.GPUs[0].Name != "Red Hat, Inc. Virtio 1.0 GPU" {
		t.Errorf("lspci fallback = %+v", h.GPUs)
	}
}

const hwWinFixture = "vendor~@~LENOVO\r\n" +
	"product~@~20Y7003AUS\r\n" +
	"memtotal~@~34038108160\r\n" +
	"cpu~@~AMD Ryzen 7 PRO 5850U with Radeon Graphics~@~8~@~16~@~1901\r\n" +
	"mem~@~DIMM 0~@~17179869184~@~26~@~3200~@~Samsung~@~M471A2K43DB1-CWE\r\n" +
	"mem~@~DIMM 1~@~17179869184~@~26~@~3200~@~Samsung~@~M471A2K43DB1-CWE\r\n" +
	"disk~@~SAMSUNG MZVL2512HCJQ-000LV~@~512110190592~@~SCSI\r\n" +
	"gpu~@~AMD Radeon(TM) Graphics~@~536870912~@~31.0.12027.9001\r\n"

func TestCollectWindowsHardware(t *testing.T) {
	h := CollectWindowsHardware(stubRunner{stdout: hwWinFixture}, "win1")
	if !h.OK {
		t.Fatalf("not ok: %s", h.Error)
	}
	if h.Vendor != "LENOVO" || h.Product != "20Y7003AUS" {
		t.Errorf("machine = %q %q", h.Vendor, h.Product)
	}
	if h.CPUModel != "AMD Ryzen 7 PRO 5850U with Radeon Graphics" ||
		h.CPUSockets != 1 || h.CPUCores != 8 || h.CPUThreads != 16 || h.CPUMaxMHz != 1901 {
		t.Errorf("cpu = %+v", h)
	}
	if len(h.MemModules) != 2 || h.MemModules[0].Type != "DDR4" || h.MemModules[0].SpeedMTs != 3200 {
		t.Errorf("modules = %+v", h.MemModules)
	}
	if len(h.Disks) != 1 || h.Disks[0].Model != "SAMSUNG MZVL2512HCJQ-000LV" || h.Disks[0].Kind != "" {
		t.Errorf("disks = %+v (windows cannot tell SSD from HDD)", h.Disks)
	}
	if len(h.GPUs) != 1 || h.GPUs[0].MemTotalMB != 512 {
		t.Errorf("gpus = %+v", h.GPUs)
	}
}
