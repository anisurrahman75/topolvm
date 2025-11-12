package mounter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/topolvm/topolvm/api/v1"
	"github.com/topolvm/topolvm/internal/filesystem"
	"github.com/topolvm/topolvm/pkg/lvmd/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	mountutil "k8s.io/mount-utils"
	utilexec "k8s.io/utils/exec"
	ctrl "sigs.k8s.io/controller-runtime"
)

var mountLogger = ctrl.Log.WithName("driver").WithName("mounter")

type LVMount struct {
	nodeName  string
	client    proto.VGServiceClient
	lvService proto.LVServiceClient
	mounter   mountutil.SafeFormatAndMount
}

type MountResponse struct {
	DevicePath     string
	MountPath      string
	FSType         string
	MountOptions   []string
	AlreadyMounted bool
	Success        bool
	Message        string
}

func NewLVMount(vgService proto.VGServiceClient, lvService proto.LVServiceClient) *LVMount {
	return &LVMount{
		client:    vgService,
		lvService: lvService,
		mounter: mountutil.SafeFormatAndMount{
			Interface: mountutil.New(""),
			Exec:      utilexec.New(),
		},
	}
}

// Mount performs an idempotent mount of the LV snapshot.
func (m *LVMount) Mount(ctx context.Context, k8sLV *v1.LogicalVolume) (*MountResponse, error) {
	resp := &MountResponse{}

	lv, err := m.fetchLogicalVolume(ctx, k8sLV)
	if err != nil {
		return resp, err
	}
	devicePath := lv.GetPath()

	mountPath := getMountPathFromLV(lv.GetName())

	fsType, err := filesystem.DetectFilesystem(devicePath)
	if err != nil {
		return resp, fmt.Errorf("failed to detect filesystem for LV %s: %v", k8sLV.Name, err)
	}

	mountOptions := []string{"ro", "norecovery"}

	resp.DevicePath = devicePath
	resp.MountPath = mountPath
	resp.FSType = fsType
	resp.MountOptions = mountOptions

	mountLogger.Info("Mounting LV snapshot",
		"volume", k8sLV.Name,
		"device", devicePath,
		"mountPath", mountPath,
		"fsType", fsType,
		"options", mountOptions,
	)

	if err := m.prepareMountDir(mountPath); err != nil && !os.IsExist(err) {
		return m.fail(resp, fmt.Sprintf("failed to create mount directory: %v", err))
	}

	alreadyMounted, err := m.checkAlreadyMounted(mountPath)
	if err != nil {
		return m.fail(resp, fmt.Sprintf("failed to check mount point: %v", err))
	}
	if alreadyMounted {
		resp.AlreadyMounted = true
		resp.Success = true
		resp.Message = "LV snapshot already mounted"
		mountLogger.Info("Already mounted", "target", mountPath)
		return resp, nil
	}
	if err := m.doMount(devicePath, mountPath, fsType, mountOptions); err != nil {
		return m.fail(resp, fmt.Sprintf("failed to mount %s: %v", devicePath, err))
	}

	resp.Success = true
	resp.Message = "Snapshot mounted successfully"
	mountLogger.Info("Mounted successfully", "device", devicePath, "target", mountPath)
	return resp, nil
}

func (m *LVMount) fetchLogicalVolume(ctx context.Context, k8sLV *v1.LogicalVolume) (*proto.LogicalVolume, error) {
	lv, err := m.getLogicalVolume(ctx, k8sLV.Spec.DeviceClass, k8sLV.Status.VolumeID)
	if err != nil {
		return nil, err
	}
	if lv == nil {
		return nil, fmt.Errorf("failed to find the logical volume for k8s LV %s", k8sLV.Name)
	}
	return lv, nil
}

func getMountPathFromLV(lvName string) string {
	baseDir := os.Getenv("ONLINE_SNAPSHOT_DIR")
	return filepath.Join(baseDir, lvName)
}

func (m *LVMount) prepareMountDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (m *LVMount) checkAlreadyMounted(target string) (bool, error) {
	isMnt, err := m.mounter.IsMountPoint(target)
	if err != nil {
		return true, err
	}
	return isMnt, nil
}

func (m *LVMount) doMount(device, target, fsType string, options []string) error {
	return m.mounter.Mount(device, target, fsType, options)
}

func (m *LVMount) fail(resp *MountResponse, msg string) (*MountResponse, error) {
	resp.Success = false
	resp.Message = msg
	return resp, fmt.Errorf(msg)
}

func (m *LVMount) getLogicalVolume(ctx context.Context, deviceClass, volumeID string) (*proto.LogicalVolume, error) {
	listResp, err := m.client.GetLVList(ctx, &proto.GetLVListRequest{DeviceClass: deviceClass})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list LV: %v", err)
	}
	return m.findVolumeByID(listResp, volumeID), nil
}

func (m *LVMount) findVolumeByID(listResp *proto.GetLVListResponse, name string) *proto.LogicalVolume {
	for _, v := range listResp.Volumes {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// Unmount performs an idempotent unmount of the LV snapshot.
func (m *LVMount) Unmount(ctx context.Context, k8sLV *v1.LogicalVolume) error {
	lv, err := m.fetchLogicalVolume(ctx, k8sLV)
	if err != nil {
		// If the LV doesn't exist, consider it already unmounted
		if status.Code(err) == codes.NotFound {
			mountLogger.Info("LV not found, skipping unmount", "volume", k8sLV.Name)
			return nil
		}
		return fmt.Errorf("failed to fetch logical volume for unmount: %v", err)
	}

	mountPath := getMountPathFromLV(lv.GetName())

	mountLogger.Info("Unmounting LV snapshot",
		"volume", k8sLV.Name,
		"mountPath", mountPath,
	)

	// Check if it's mounted
	isMounted, err := m.mounter.IsMountPoint(mountPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Mount point doesn't exist, consider it already unmounted
			mountLogger.Info("Mount path doesn't exist, skipping unmount", "mountPath", mountPath)
			return nil
		}
		return fmt.Errorf("failed to check mount point: %v", err)
	}

	if !isMounted {
		mountLogger.Info("LV not mounted, skipping unmount", "mountPath", mountPath)
		return nil
	}

	// Perform the unmount
	if err := m.mounter.Unmount(mountPath); err != nil {
		return fmt.Errorf("failed to unmount %s: %v", mountPath, err)
	}

	mountLogger.Info("Unmounted successfully", "mountPath", mountPath)

	// Optionally remove the mount directory
	if err := os.Remove(mountPath); err != nil && !os.IsNotExist(err) {
		mountLogger.Info("Warning: failed to remove mount directory", "mountPath", mountPath, "error", err)
		// Don't return error here as unmount was successful
	}

	return nil
}
