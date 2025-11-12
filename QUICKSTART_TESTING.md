# Quick Start Guide: Testing MountLV Locally

## ✅ YES! You can test this locally

I've created comprehensive testing infrastructure for you:

## Three Ways to Test:

### 1. 🚀 Quick Automated Test (Recommended)

Run the interactive shell script that demonstrates all MountLV features:

```bash
sudo ./test_mountlv.sh
```

This script will:
- Create a test LVM setup using loop devices
- Test mounting with ext4 (default)
- Test mounting with custom options (noatime)
- Test read-only mounting
- Test XFS filesystem (if available)
- Automatically clean up everything

**Duration:** ~30 seconds

---

### 2. 🧪 Run Unit Tests

Run the comprehensive Go unit tests:

```bash
# Run all MountLV tests
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV

# Run specific test scenarios
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountWithDefaultFS
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountWithXFS
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountReadOnly
```

**Duration:** ~1-2 minutes

The unit tests cover:
- ✅ Default ext4 mounting with rw mode
- ✅ XFS mounting with nouuid option
- ✅ Custom mount options (noatime, nodiratime)
- ✅ Read-only mounting
- ✅ Idempotency (remounting already-mounted volumes)
- ✅ Error handling (non-existent volumes, invalid parameters)

---

### 3. 🔧 Manual Testing

If you want to test manually with your own LVM setup:

```bash
# 1. Ensure you have LVM volumes
sudo lvs

# 2. Build the lvmd binary
go build ./cmd/lvmd/

# 3. Start lvmd (in one terminal)
sudo ./lvmd --config=/path/to/lvmd.yaml

# 4. Use grpcurl or write a client to call MountLV RPC
```

---

## Prerequisites

Before testing, ensure you have:

```bash
# Ubuntu/Debian
sudo apt-get install -y lvm2 xfsprogs e2fsprogs

# RHEL/CentOS/Fedora  
sudo dnf install -y lvm2 xfsprogs e2fsprogs
```

---

## Quick Test Run

Try this one-liner to see if everything works:

```bash
sudo ./test_mountlv.sh
```

Expected output:
```
=== MountLV RPC Interactive Test ===

✓ All required tools found

Step 1: Creating loop device...
✓ Created loop device: /dev/loop0

Step 2: Setting up LVM...
✓ Created logical volume: /dev/mountlv_test_vg/mountlv_test_lv

=== Test 1: Mount with default ext4 filesystem ===
✓ Successfully mounted at /tmp/mountlv_test
✓ Write test successful
✓ Unmounted successfully

=== Test 2: Mount with custom options (noatime) ===
✓ Custom mount option applied
✓ Data persisted across mounts

=== Test 3: Mount in read-only mode ===
✓ Mounted in read-only mode
✓ Write correctly blocked in read-only mode

=== Test 4: Mount with XFS filesystem ===
✓ Successfully mounted with XFS
✓ Write test on XFS successful

=== Test Summary ===
✓ All tests completed successfully!
```

---

## Troubleshooting

**Permission Denied?**
→ Use `sudo`

**Loop device error?**
→ `sudo modprobe loop`

**mkfs.xfs not found?**
→ `sudo apt-get install xfsprogs`

**Tests won't clean up?**
→ Manual cleanup:
```bash
sudo umount /tmp/test_*
sudo vgremove -f test_lvservice mountlv_test_vg
sudo losetup -D
```

---

## What Gets Tested?

✅ **Filesystem Detection** - Detects existing filesystems  
✅ **Automatic Formatting** - Formats devices if no filesystem exists  
✅ **Mount Execution** - Executes actual mount commands  
✅ **Read-Write Mode** - Default rw mounting  
✅ **Read-Only Mode** - ro mounting when specified  
✅ **XFS Support** - With automatic nouuid option  
✅ **Custom Options** - noatime, nodiratime, etc.  
✅ **Idempotency** - Safe to call multiple times  
✅ **Error Handling** - Proper errors for invalid requests  

---

## Files Created

1. **`test_mountlv.sh`** - Interactive shell script
2. **`TEST_MOUNTLV_LOCALLY.md`** - Detailed testing guide
3. **`TestLVService_MountLV`** in `lvservice_test.go` - Unit tests

All ready to use! 🎉

