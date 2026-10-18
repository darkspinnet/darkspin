//go:build scenario && windows

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	entryCombase           = windows.NewLazySystemDLL("combase.dll")
	entryD3D11             = windows.NewLazySystemDLL("d3d11.dll")
	entryOle32             = windows.NewLazySystemDLL("ole32.dll")
	entryKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	entryApartmentType     = entryOle32.NewProc("CoGetApartmentType")
	entryCurrentThread     = entryKernel32.NewProc("GetCurrentThreadId")
	entryRoInitialize      = entryCombase.NewProc("RoInitialize")
	entryRoUninitialize    = entryCombase.NewProc("RoUninitialize")
	entryCreateString      = entryCombase.NewProc("WindowsCreateString")
	entryDeleteString      = entryCombase.NewProc("WindowsDeleteString")
	entryActivationFactory = entryCombase.NewProc("RoGetActivationFactory")
	entryCreateDevice      = entryD3D11.NewProc("D3D11CreateDevice")
	entryCreateWinRTDevice = entryD3D11.NewProc("CreateDirect3D11DeviceFromDXGIDevice")
)

// These are installed SDK identities, not client vtables or object pointers.
var (
	entryIIDItemInterop    = windows.GUID{Data1: 0x3628e81b, Data2: 0x3cac, Data3: 0x4c60, Data4: [8]byte{0xb7, 0xf4, 0x23, 0xce, 0x0e, 0x0c, 0x33, 0x56}}
	entryIIDItem           = windows.GUID{Data1: 0x79c3f95b, Data2: 0x31f7, Data3: 0x4ec2, Data4: [8]byte{0xa4, 0x64, 0x63, 0x2e, 0xf5, 0xd3, 0x07, 0x60}}
	entryIIDPoolStatics    = windows.GUID{Data1: 0x589b103f, Data2: 0x6bbc, Data3: 0x5df5, Data4: [8]byte{0xa9, 0x91, 0x02, 0xe2, 0x8b, 0x3b, 0x66, 0xd5}}
	entryIIDSessionStatics = windows.GUID{Data1: 0x2224a540, Data2: 0x5974, Data3: 0x49aa, Data4: [8]byte{0xb2, 0x32, 0x08, 0x82, 0x53, 0x6f, 0x4c, 0xb5}}
	entryIIDClosable       = windows.GUID{Data1: 0x30d5a829, Data2: 0x7fa4, Data3: 0x4026, Data4: [8]byte{0x83, 0xbb, 0xd7, 0x5b, 0xae, 0x4e, 0xa9, 0x9e}}
	entryIIDDXGIDevice     = windows.GUID{Data1: 0x54ec77fa, Data2: 0x1377, Data3: 0x44e6, Data4: [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	entryIIDDirectDevice   = windows.GUID{Data1: 0xa37624ab, Data2: 0x8d5f, Data3: 0x4650, Data4: [8]byte{0x9d, 0x3e, 0x9e, 0xae, 0x3d, 0x9b, 0xc6, 0x70}}
	entryIIDDXGIAccess     = windows.GUID{Data1: 0xa9b3d012, Data2: 0x3df2, Data3: 0x4ee3, Data4: [8]byte{0xb8, 0xd1, 0x86, 0x95, 0xf4, 0x57, 0xd3, 0xc1}}
	entryIIDTexture        = windows.GUID{Data1: 0x6f15aaf2, Data2: 0xd208, Data3: 0x4e89, Data4: [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
)

type scenarioWGCAttempt struct {
	isInitialized       bool
	resources           []uintptr
	strings             []uintptr
	device              uintptr
	deviceContext       uintptr
	item                uintptr
	pool                uintptr
	session             uintptr
	frame               uintptr
	isItemSizeAvailable bool
}

// HRESULT uses low signed32 bits; the syscall LastError/EDX slots have no
// HRESULT/void COM contract. Release returning reference count zero is valid.
//
//go:uintptrescapes
func callScenarioWGC(pointer uintptr, slot uintptr, arguments ...uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(pointer))
	method := *(*uintptr)(unsafe.Pointer(vtable + slot*unsafe.Sizeof(uintptr(0))))
	words := append([]uintptr{pointer}, arguments...)
	result, secondary, callErr := syscall.SyscallN(method, words...)
	_ = secondary
	if callErr != syscall.Errno(0) {
		// COM HRESULT/void returns do not define Win32 LastError.
	}
	return result
}

func scenarioWGCHRESULT(status uintptr) error {
	if int32(uint32(status)) < 0 {
		return fmt.Errorf("HRESULT 0x%08x", uint32(status))
	}
	return nil
}

//go:uintptrescapes
func callScenarioWGCProc(proc *windows.LazyProc, arguments ...uintptr) uintptr {
	status, secondary, callErr := proc.Call(arguments...)
	_ = secondary
	if callErr != syscall.Errno(0) {
		// These WinRT/D3D entry points return HRESULT, never LastError.
	}
	return status
}

func (e *scenarioWGCAttempt) retain(pointer uintptr) {
	if pointer != 0 {
		e.resources = append(e.resources, pointer)
	}
}

func (e *scenarioWGCAttempt) query(ctx context.Context, pointer uintptr, iid *windows.GUID) (uintptr, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("wgcQueryContext: %w", err)
	}
	var result uintptr
	status := callScenarioWGC(pointer, 0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&result)))
	runtime.KeepAlive(iid)
	e.retain(result)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return 0, fmt.Errorf("wgcQuery: %w", err)
	}
	if result == 0 {
		return 0, errors.New("WGC QueryInterface returned no interface")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("wgcQueryFinal: %w", err)
	}
	return result, nil
}

func (e *scenarioWGCAttempt) initialize(ctx context.Context, capture *scenarioEntryWindowCapture) error {
	if runtime.GOARCH != "386" {
		return errors.New("WGC by-value ABI is supported only for Windows386")
	}
	for _, proc := range []*windows.LazyProc{entryRoInitialize, entryRoUninitialize, entryCreateString, entryDeleteString, entryActivationFactory, entryCreateDevice, entryCreateWinRTDevice} {
		contextErr := scenarioEntryContext(ctx)
		if contextErr != nil {
			return fmt.Errorf("wgcSupportContext: %w", contextErr)
		}
		err := proc.Find()
		if err != nil {
			return fmt.Errorf("wgcSupport[%s]: %w", proc.Name, err)
		}
	}
	err := scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcInitializeContext: %w", err)
	}
	status := callScenarioWGCProc(entryRoInitialize, 1) // RO_INIT_MULTITHREADED; no mode change fallback.
	capture.RoInitializeHRESULT, capture.IsRoInitializeHRESULTAvailable = int32(uint32(status)), true
	if uint32(status) != 0 && uint32(status) != 1 {
		return fmt.Errorf("wgcApartment: unsupported HRESULT 0x%08x", uint32(status))
	}
	e.isInitialized = true // Balance both S_OK and S_FALSE on this same thread.
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcApartmentContext: %w", err)
	}
	capture.ApartmentInitialized, err = observeScenarioWindowApartment()
	if err != nil {
		return fmt.Errorf("wgcApartmentRead: %w", err)
	}
	err = validateScenarioWindowCaptureApartment(capture.ApartmentInitialized, false)
	if err != nil {
		return fmt.Errorf("wgcApartmentMode: %w", err)
	}
	if capture.ApartmentBefore.ThreadID != capture.ApartmentInitialized.ThreadID {
		return errors.New("WGC initialized on a different thread")
	}
	statics, err := e.factory(ctx, "Windows.Graphics.Capture.GraphicsCaptureSession", &entryIIDSessionStatics)
	if err != nil {
		return fmt.Errorf("wgcSessionFactory: %w", err)
	}
	var isSupported uint8 // WinRT boolean is one byte, not Windows BOOL.
	status = callScenarioWGC(statics, 6, uintptr(unsafe.Pointer(&isSupported)))
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return fmt.Errorf("wgcSupported: %w", err)
	}
	if isSupported != 1 {
		return errors.New("Windows Graphics Capture is unsupported")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return fmt.Errorf("wgcInitializeFinal: %w", err)
	}
	return nil
}

// Fixed installed API reads only. Failed HRESULTs never expose enum outputs;
// LastError is not part of either API's documented result contract.
func observeScenarioWindowApartment() (scenarioWindowApartment, error) {
	var observation scenarioWindowApartment
	var queryErrs []error
	err := entryCurrentThread.Find()
	observation.IsThreadProcedureAvailabilityKnown = true
	if err != nil {
		queryErrs = append(queryErrs, fmt.Errorf("wgcThreadProcedure: %w", err))
	} else {
		observation.IsThreadProcedureAvailable = true
		observation.ThreadID = uint32(callScenarioWGCProc(entryCurrentThread))
		observation.IsThreadIDAvailable = observation.ThreadID != 0
		if !observation.IsThreadIDAvailable {
			queryErrs = append(queryErrs, errors.New("current thread ID is zero"))
		}
	}
	err = entryApartmentType.Find()
	observation.IsApartmentProcedureAvailabilityKnown = true
	if err != nil {
		queryErrs = append(queryErrs, fmt.Errorf("wgcApartmentProcedure: %w", err))
	} else {
		observation.IsApartmentProcedureAvailable = true
		var apartmentType, qualifier int32 // Two documented 32-bit enum outputs.
		status := uint32(callScenarioWGCProc(entryApartmentType, uintptr(unsafe.Pointer(&apartmentType)), uintptr(unsafe.Pointer(&qualifier))))
		observation.HRESULT, observation.IsHRESULTAvailable = int32(status), true
		switch status {
		case 0: // CoGetApartmentType documents S_OK as its successful result.
			observation.ApartmentType, observation.IsApartmentTypeAvailable = apartmentType, true
			observation.Qualifier, observation.IsQualifierAvailable = qualifier, true
		case 0x800401f0: // CO_E_NOTINITIALIZED: no enum outputs are available.
		default:
			queryErrs = append(queryErrs, fmt.Errorf("wgcApartmentQuery: unsupported HRESULT 0x%08x", status))
		}
	}
	if len(queryErrs) > 0 {
		return observation, fmt.Errorf("wgcApartmentObserve: %w", errors.Join(queryErrs...))
	}
	return observation, nil
}

func validateScenarioWindowCaptureApartment(observation scenarioWindowApartment, isUninitializedAllowed bool) error {
	if !observation.IsThreadIDAvailable || !observation.IsHRESULTAvailable ||
		!observation.IsThreadProcedureAvailabilityKnown || !observation.IsThreadProcedureAvailable ||
		!observation.IsApartmentProcedureAvailabilityKnown || !observation.IsApartmentProcedureAvailable {
		return errors.New("capture apartment/thread procedure or result is unavailable")
	}
	if uint32(observation.HRESULT) == 0x800401f0 && isUninitializedAllowed &&
		!observation.IsApartmentTypeAvailable && !observation.IsQualifierAvailable {
		return nil
	}
	if observation.HRESULT != 0 || !observation.IsApartmentTypeAvailable || !observation.IsQualifierAvailable ||
		observation.ApartmentType != 1 || (observation.Qualifier != 0 && observation.Qualifier != 1) {
		return errors.New("capture requires documented MTA or permitted uninitialized apartment")
	}
	return nil
}

func validateScenarioWindowCallerApartment(observation scenarioWindowApartment) error {
	if !observation.IsThreadIDAvailable || !observation.IsHRESULTAvailable || observation.HRESULT != 0 ||
		!observation.IsThreadProcedureAvailabilityKnown || !observation.IsThreadProcedureAvailable ||
		!observation.IsApartmentProcedureAvailabilityKnown || !observation.IsApartmentProcedureAvailable ||
		!observation.IsApartmentTypeAvailable || !observation.IsQualifierAvailable ||
		observation.ApartmentType < 0 || observation.ApartmentType > 3 || observation.Qualifier < 0 || observation.Qualifier > 6 {
		return errors.New("caller requires a documented successful apartment/thread descriptor")
	}
	return nil
}

func (e *scenarioWGCAttempt) factory(ctx context.Context, class string, iid *windows.GUID) (uintptr, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("wgcFactoryContext: %w", err)
	}
	chars, err := windows.UTF16FromString(class)
	if err != nil {
		return 0, fmt.Errorf("wgcClass: %w", err)
	}
	var hstring uintptr
	status := callScenarioWGCProc(entryCreateString, uintptr(unsafe.Pointer(&chars[0])), uintptr(len(chars)-1), uintptr(unsafe.Pointer(&hstring)))
	runtime.KeepAlive(chars)
	if hstring != 0 {
		e.strings = append(e.strings, hstring)
	}
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return 0, fmt.Errorf("wgcString: %w", err)
	}
	if hstring == 0 {
		return 0, errors.New("WGC class string is missing")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("wgcActivationContext: %w", err)
	}
	var factory uintptr
	status = callScenarioWGCProc(entryActivationFactory, hstring, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&factory)))
	runtime.KeepAlive(iid)
	e.retain(factory)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return 0, fmt.Errorf("wgcActivation: %w", err)
	}
	if factory == 0 {
		return 0, errors.New("WGC activation returned no factory")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("wgcFactoryFinal: %w", err)
	}
	return factory, nil
}

func (e *scenarioWGCAttempt) create(ctx context.Context, window uintptr) (scenarioWindowSize, error) {
	err := scenarioEntryContext(ctx)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcDeviceContext: %w", err)
	}
	var featureLevel uint32
	status := callScenarioWGCProc(entryCreateDevice, 0, 1, 0, 0x20, 0, 0, 7,
		uintptr(unsafe.Pointer(&e.device)), uintptr(unsafe.Pointer(&featureLevel)), uintptr(unsafe.Pointer(&e.deviceContext)))
	e.retain(e.device)
	e.retain(e.deviceContext)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcDevice: %w", err)
	}
	if e.device == 0 || e.deviceContext == 0 {
		return scenarioWindowSize{}, errors.New("WGC hardware device/context is missing")
	}
	dxgi, err := e.query(ctx, e.device, &entryIIDDXGIDevice)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcDXGI: %w", err)
	}
	var inspectable uintptr
	status = callScenarioWGCProc(entryCreateWinRTDevice, dxgi, uintptr(unsafe.Pointer(&inspectable)))
	e.retain(inspectable)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcWrapDevice: %w", err)
	}
	if inspectable == 0 {
		return scenarioWindowSize{}, errors.New("WGC device wrapper is missing")
	}
	directDevice, err := e.query(ctx, inspectable, &entryIIDDirectDevice)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcDirectDevice: %w", err)
	}
	factory, err := e.factory(ctx, "Windows.Graphics.Capture.GraphicsCaptureItem", &entryIIDItemInterop)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcItemFactory: %w", err)
	}
	status = callScenarioWGC(factory, 3, window, uintptr(unsafe.Pointer(&entryIIDItem)), uintptr(unsafe.Pointer(&e.item)))
	e.retain(e.item)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return scenarioWindowSize{}, fmt.Errorf("wgcOwnItem: %w", err)
	}
	if e.item == 0 {
		return scenarioWindowSize{}, errors.New("WGC owned item is missing")
	}
	size, err := e.itemSize(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcInitialSize: %w", err)
	}
	factory, err = e.factory(ctx, "Windows.Graphics.Capture.Direct3D11CaptureFramePool", &entryIIDPoolStatics)
	if err != nil {
		return size, fmt.Errorf("wgcPoolFactory: %w", err)
	}
	// Reviewed x86 SizeInt32 is BY VALUE: width/height occupy two stack words.
	status = callScenarioWGC(factory, 6, directDevice, 87, 1, uintptr(size.Width), uintptr(size.Height), uintptr(unsafe.Pointer(&e.pool)))
	e.retain(e.pool)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return size, fmt.Errorf("wgcPool: %w", err)
	}
	if e.pool == 0 {
		return size, errors.New("WGC frame pool is missing")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcSessionContext: %w", err)
	}
	status = callScenarioWGC(e.pool, 10, e.item, uintptr(unsafe.Pointer(&e.session)))
	e.retain(e.session)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return size, fmt.Errorf("wgcSession: %w", err)
	}
	if e.session == 0 {
		return size, errors.New("WGC capture session is missing")
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcStartContext: %w", err)
	}
	status = callScenarioWGC(e.session, 6)
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return size, fmt.Errorf("wgcStart: %w", err)
	}
	err = scenarioEntryContext(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcCreateFinal: %w", err)
	}
	return size, nil
}

func (e *scenarioWGCAttempt) itemSize(ctx context.Context) (scenarioWindowSize, error) {
	var size scenarioWindowSize
	e.isItemSizeAvailable = false
	err := scenarioEntryContext(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcItemContext: %w", err)
	}
	status := callScenarioWGC(e.item, 7, uintptr(unsafe.Pointer(&size)))
	err = scenarioWGCHRESULT(status)
	if err != nil {
		return size, fmt.Errorf("wgcItemSize: %w", err)
	}
	e.isItemSizeAvailable = true
	err = scenarioEntryContext(ctx)
	if err != nil {
		return size, fmt.Errorf("wgcItemFinal: %w", err)
	}
	err = validateScenarioWindowSize(size)
	if err != nil {
		return size, fmt.Errorf("wgcItemBounds: %w", err)
	}
	return size, nil
}

func (e *scenarioWGCAttempt) close() error {
	var cleanupErrs []error
	for _, pointer := range []uintptr{e.frame, e.session, e.pool} {
		if pointer == 0 {
			continue
		}
		var closable uintptr
		status := callScenarioWGC(pointer, 0, uintptr(unsafe.Pointer(&entryIIDClosable)), uintptr(unsafe.Pointer(&closable)))
		queryErr := scenarioWGCHRESULT(status)
		if queryErr != nil || closable == 0 {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("wgcCloseQuery: %w", errors.Join(queryErr, errors.New("closable interface unavailable"))))
		} else {
			status = callScenarioWGC(closable, 6)
			closeErr := scenarioWGCHRESULT(status)
			if closeErr != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("wgcClose: %w", closeErr))
			}
		}
		if closable != 0 {
			callScenarioWGC(closable, 2)
		}
	}
	for index := len(e.resources) - 1; index >= 0; index-- {
		callScenarioWGC(e.resources[index], 2)
	}
	for index := len(e.strings) - 1; index >= 0; index-- {
		status := callScenarioWGCProc(entryDeleteString, e.strings[index])
		err := scenarioWGCHRESULT(status)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("wgcDeleteString: %w", err))
		}
	}
	if e.isInitialized {
		callScenarioWGCProc(entryRoUninitialize)
	}
	if len(cleanupErrs) > 0 {
		return fmt.Errorf("wgcCleanup: %w", errors.Join(cleanupErrs...))
	}
	return nil
}
