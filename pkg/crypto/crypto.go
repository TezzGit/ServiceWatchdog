package crypto

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Encryption interface {
	Decrypt(encrypted []byte) ([]byte, error)
	Encrypt(plain []byte) ([]byte, error)
}

type NoEncoding struct{}

type DPAPI struct{}

func (none *NoEncoding) Decrypt(encrypted []byte) ([]byte, error) {
	return encrypted, nil
}

func (d *DPAPI) Decrypt(encrypted []byte) ([]byte, error) {
	var out windows.DataBlob
	var in windows.DataBlob

	in.Size = uint32(len(encrypted))
	if len(encrypted) > 0 {
		in.Data = &encrypted[0]
	}

	r, _, err := syscall.NewLazyDLL("crypt32.dll").
		NewProc("CryptUnprotectData").
		Call(
			uintptr(unsafe.Pointer(&in)),
			0,
			0,
			0,
			0,
			0,
			uintptr(unsafe.Pointer(&out)),
		)

	if r == 0 {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	return unsafe.Slice(out.Data, out.Size), nil
}

func (none *NoEncoding) Encrypt(plain []byte) ([]byte, error) {
	return plain, nil
}

func (d *DPAPI) Encrypt(plain []byte) ([]byte, error) {
	var out windows.DataBlob
	var in windows.DataBlob

	in.Size = uint32(len(plain))
	if len(plain) > 0 {
		in.Data = &plain[0]
	}

	// Call CryptProtectData to encrypt
	r, _, err := syscall.NewLazyDLL("crypt32.dll").
		NewProc("CryptProtectData").
		Call(
			uintptr(unsafe.Pointer(&in)), // Data to encrypt
			0,                            // Description (optional)
			0,                            // Optional entropy (optional)
			0,                            // Reserved, must be zero
			0,                            // Prompt structure (optional)
			0,                            // Flags (0 = no UI)
			uintptr(unsafe.Pointer(&out)),
		)

	if r == 0 {
		return nil, err
	}

	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	encrypted := unsafe.Slice(out.Data, out.Size)
	return encrypted, nil
}

func NewEncryption(encryptionType string) (Encryption, error) {
	switch encryptionType {
	case "DPAPI":
		return &DPAPI{}, nil
	case "None", "", "none":
		return &NoEncoding{}, nil
	default:
		return nil, errors.New("unsupported encryption type")
	}

}
