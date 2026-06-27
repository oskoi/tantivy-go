package internal

/*
#include "../bindings.h"
#include <stdlib.h>
*/
import "C"
import (
	"errors"
	"unsafe"
)

func freeCString(value *C.char) {
	if value != nil {
		C.free(unsafe.Pointer(value))
	}
}

// LibInit for tests init
func LibInit(cleanOnPanic, utf8Lenient bool, directive ...string) error {
	var initVal string
	if len(directive) == 0 {
		initVal = "info"
	} else {
		initVal = directive[0]
	}

	cInitVal := C.CString(initVal)
	defer freeCString(cInitVal)
	cCleanOnPanic := C.bool(cleanOnPanic)
	cUtf8Lenient := C.bool(utf8Lenient)
	var errBuffer *C.char
	C.init_lib(cInitVal, &errBuffer, cCleanOnPanic, cUtf8Lenient)

	if errBuffer == nil {
		return nil
	}
	errorMessage := C.GoString(errBuffer)
	C.string_free(errBuffer)
	if errorMessage != "" {
		return errors.New(errorMessage)
	}
	return nil
}
