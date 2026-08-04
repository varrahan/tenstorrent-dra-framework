package lifecycle

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// Values from the public tt-kmd ioctl.h RESET_DEVICE ABI.
	ttIoctlResetDevice = uintptr(0xfa<<8 | 6)
	ttResetASIC        = uint32(4)
	ttResetPost        = uint32(6)
)

type Resetter interface {
	Reset(context.Context, string) error
}

type ResetFunc func(context.Context, string) error

// Reset adapts a function to the Resetter interface.
func (f ResetFunc) Reset(ctx context.Context, path string) error { return f(ctx, path) }

type KMDResetter struct{}

// Reset performs the tt-kmd ASIC and post-reset ioctl sequence on one device.
func (KMDResetter) Reset(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer unix.Close(fd)
	if err := resetIoctl(fd, ttResetASIC); err != nil {
		return fmt.Errorf("ASIC reset %s: %w", path, err)
	}
	if err := resetIoctl(fd, ttResetPost); err != nil {
		return fmt.Errorf("post-reset %s: %w", path, err)
	}
	return nil
}

type NoopResetter struct{}

// Reset intentionally performs no hardware operation for synthetic validation environments.
func (NoopResetter) Reset(context.Context, string) error { return nil }

type resetRequest struct {
	OutputSize uint32
	Flags      uint32
	ResultSize uint32
	Result     uint32
}

// resetIoctl submits one tt-kmd reset request and checks both syscall and driver results.
func resetIoctl(fd int, flag uint32) error {
	request := resetRequest{OutputSize: 8, Flags: flag}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), ttIoctlResetDevice, uintptr(unsafe.Pointer(&request)))
	if errno != 0 {
		return errno
	}
	if request.Result != 0 {
		return fmt.Errorf("tt-kmd reset result %d", request.Result)
	}
	return nil
}
