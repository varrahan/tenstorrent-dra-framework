package device

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const ttIoctlGetDriverInfo = uintptr(0xfa<<8 | 5)

type driverInfoRequest struct {
	RequestedOutputSize uint32
	ReturnedOutputSize  uint32
	DriverVersion       uint32
	Major               uint8
	Minor               uint8
	Patch               uint8
	Reserved            uint8
}

// readKMDInfo reads the public tt-kmd driver version and ioctl ABI from a device.
func readKMDInfo(path string) (string, uint32, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", 0, err
	}
	defer unix.Close(fd)
	request := driverInfoRequest{RequestedOutputSize: 12}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), ttIoctlGetDriverInfo, uintptr(unsafe.Pointer(&request)))
	if errno != 0 {
		return "", 0, errno
	}
	if request.ReturnedOutputSize < 12 {
		return "", 0, fmt.Errorf("tt-kmd returned %d driver-info bytes", request.ReturnedOutputSize)
	}
	return fmt.Sprintf("%d.%d.%d", request.Major, request.Minor, request.Patch), request.DriverVersion, nil
}
