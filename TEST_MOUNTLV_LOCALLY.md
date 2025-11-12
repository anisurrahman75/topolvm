# Testing MountLV RPC Locally

## Prerequisites

1. **Root/sudo access** - Required for LVM and mount operations
2. **LVM utilities** - `lvm2`, `mkfs.ext4`, `mkfs.xfs`
3. **Go 1.19+**
4. **Available disk space** - At least 2GB for test loop devices

## Installation of Required Packages

```bash
# On Ubuntu/Debian
sudo apt-get update
sudo apt-get install -y lvm2 xfsprogs e2fsprogs

# On RHEL/CentOS/Fedora
sudo dnf install -y lvm2 xfsprogs e2fsprogs
```

## Option 1: Run the Unit Tests

The easiest way to test locally is to run the comprehensive unit tests:

```bash
cd /home/anisur/go/src/github.com/anisurrahman75/topolvm

# Run all lvservice tests (including MountLV)
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV

# Or run specific sub-tests
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountWithDefaultFS
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountWithXFS
sudo go test -v ./internal/lvmd/ -run TestLVService_MountLV/MountReadOnly
```

**Note:** Tests must be run with `sudo` because they require root privileges for LVM operations.

## Option 2: Manual Testing with a Test Script

I'll create a standalone test script below that you can run to test the MountLV functionality interactively.

## What the Tests Cover

The comprehensive test suite (`TestLVService_MountLV`) covers:

1. **Default ext4 mounting** - Tests mounting with default filesystem
2. **XFS mounting** - Tests XFS filesystem with nouuid option
3. **Custom mount options** - Tests noatime, nodiratime, etc.
4. **Read-only mounting** - Tests ro mode
5. **Read-write mounting** - Tests rw mode (default)
6. **Idempotency** - Tests remounting already mounted volumes
7. **Error handling** - Tests non-existent volumes and invalid parameters

## Expected Test Output

When you run the tests successfully, you should see:

```
=== RUN   TestLVService_MountLV
=== RUN   TestLVService_MountLV/MountWithDefaultFS
=== RUN   TestLVService_MountLV/MountWithXFS
=== RUN   TestLVService_MountLV/MountWithCustomOptions
=== RUN   TestLVService_MountLV/MountReadOnly
=== RUN   TestLVService_MountLV/MountNonExistentLV
=== RUN   TestLVService_MountLV/MountEmptyTargetPath
--- PASS: TestLVService_MountLV (X.XXs)
    --- PASS: TestLVService_MountLV/MountWithDefaultFS (X.XXs)
    --- PASS: TestLVService_MountLV/MountWithXFS (X.XXs)
    --- PASS: TestLVService_MountLV/MountWithCustomOptions (X.XXs)
    --- PASS: TestLVService_MountLV/MountReadOnly (X.XXs)
    --- PASS: TestLVService_MountLV/MountNonExistentLV (X.XXs)
    --- PASS: TestLVService_MountLV/MountEmptyTargetPath (X.XXs)
PASS
```

## Troubleshooting

### Permission Denied
- **Solution**: Run tests with `sudo`

### Loop device creation failed
- **Solution**: Load loop module: `sudo modprobe loop`

### mkfs.xfs not found
- **Solution**: Install xfsprogs: `sudo apt-get install xfsprogs`

### Tests hang or timeout
- **Solution**: Check for existing LVM volumes: `sudo lvs`
- Clean up manually: `sudo vgremove -f test_lvservice`

## Cleanup After Testing

The tests automatically clean up, but if they fail, you can manually clean up:

```bash
# Unmount any test mounts
sudo umount /tmp/test_*

# Remove test volume groups
sudo vgremove -f test_lvservice

# Remove loop devices
sudo losetup -D
```

