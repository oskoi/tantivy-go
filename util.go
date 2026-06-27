package tantivy_go

// #include "bindings.h"
import "C"
import (
	"errors"
	"unsafe"
)

func tryExtractError(errBuffer *C.char) error {
	if errBuffer == nil {
		return nil
	}

	errorMessage := C.GoString(errBuffer)
	C.string_free(errBuffer)
	if errorMessage == "" {
		return nil
	}
	return errors.New(errorMessage)
}

func newCString(value string) (*C.char, func()) {
	cValue := C.CString(value)
	return cValue, func() {
		if cValue != nil {
			C.free(unsafe.Pointer(cValue))
		}
	}
}
