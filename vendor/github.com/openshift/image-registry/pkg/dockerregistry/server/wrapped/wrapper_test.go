package wrapped

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func zeroIn(typ reflect.Type) []reflect.Value {
	var args []reflect.Value
	for i := 0; i < typ.NumIn(); i++ {
		if i == typ.NumIn()-1 && typ.IsVariadic() {
			break
		}
		arg := reflect.Zero(typ.In(i))
		args = append(args, arg)
	}
	return args
}

func TestWrapper(t *testing.T) {
	exclude := map[string]bool{
		"BlobWriter.Close":     true,
		"BlobWriter.ID":        true,
		"BlobWriter.ReadFrom":  true,
		"BlobWriter.Size":      true,
		"BlobWriter.StartedAt": true,
		"BlobWriter.Write":     true,
		"StorageDriver.Name":   true,
		"FileWriter.Size":      true,
	}

	simplify := func(wrapper Wrapper) SimpleWrapper {
		return func(funcname string, f func() error) error {
			return wrapper(context.Background(), funcname, func(ctx context.Context) error {
				return f()
			})
		}
	}

	// check funcname
	var lastFuncName string
	captureFuncName := func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		lastFuncName = funcname
		return fmt.Errorf("don't call upstream code")
	}
	objects := []reflect.Value{
		reflect.ValueOf(&blobStore{wrapper: captureFuncName}),
		reflect.ValueOf(&manifestService{wrapper: captureFuncName}),
		reflect.ValueOf(&tagService{wrapper: captureFuncName}),
		reflect.ValueOf(&blobWriter{wrapper: captureFuncName}),
		reflect.ValueOf(&blobDescriptorService{wrapper: captureFuncName}),
		reflect.ValueOf(&storageDriver{wrapper: simplify(captureFuncName)}),
		reflect.ValueOf(&readCloser{wrapper: simplify(captureFuncName)}),
		reflect.ValueOf(&fileWriter{wrapper: simplify(captureFuncName)}),
	}
	for _, v := range objects {
		typeName := strings.Title(v.Elem().Type().Name())
		for i := 0; i < v.Type().NumMethod(); i++ {
			lastFuncName = "unhandled"

			methodName := v.Type().Method(i).Name
			funcName := fmt.Sprintf("%s.%s", typeName, methodName)

			method := v.Method(i)
			args := zeroIn(method.Type())
			func() {
				defer func() {
					// BlobWriter.Close and other unhandled methods may panic
					recover()
				}()
				method.Call(args)
			}()

			expectedFuncName := funcName
			if exclude[expectedFuncName] {
				expectedFuncName = "unhandled"
			}

			if lastFuncName != expectedFuncName {
				t.Errorf("%s: got funcname %q, want %q", funcName, lastFuncName, expectedFuncName)
			}
		}
	}

	// check calls chain
	var lastResult struct {
		c   int
		err error
	}
	checkWrapper := func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		err := f(ctx)
		lastResult.c++
		if err == nil {
			lastResult.err = fmt.Errorf("got call of unknown method, want %q", funcname)
		} else if err.Error() != funcname {
			lastResult.err = fmt.Errorf("got call of %q, want %q", err, funcname)
		}
		return nil
	}
	dummyWrapper := func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		return fmt.Errorf("%s", funcname)
	}
	objects = []reflect.Value{
		reflect.ValueOf(NewBlobStore(NewBlobStore(nil, dummyWrapper), checkWrapper)),
		reflect.ValueOf(NewManifestService(NewManifestService(nil, dummyWrapper), checkWrapper)),
		reflect.ValueOf(NewTagService(NewTagService(nil, dummyWrapper), checkWrapper)),
		reflect.ValueOf(NewBlobWriter(NewBlobWriter(nil, dummyWrapper), checkWrapper)),
		reflect.ValueOf(NewBlobDescriptorService(NewBlobDescriptorService(nil, dummyWrapper), checkWrapper)),
		reflect.ValueOf(NewStorageDriver(NewStorageDriver(nil, simplify(dummyWrapper)), simplify(checkWrapper))),
		reflect.ValueOf(NewReadCloser(NewReadCloser(nil, simplify(dummyWrapper)), simplify(checkWrapper))),
		reflect.ValueOf(NewFileWriter(NewFileWriter(nil, simplify(dummyWrapper)), simplify(checkWrapper))),
	}
	for _, v := range objects {
		typeName := strings.Title(v.Elem().Type().Name())
		for i := 0; i < v.Type().NumMethod(); i++ {
			lastResult.c = 0
			lastResult.err = nil

			methodName := v.Type().Method(i).Name
			funcName := fmt.Sprintf("%s.%s", typeName, methodName)

			if exclude[funcName] {
				continue
			}

			method := v.Method(i)
			args := zeroIn(method.Type())
			func() {
				defer func() {
					// BlobWriter.Close and other unhandled methods may panic
					recover()
				}()
				method.Call(args)
			}()

			if lastResult.c != 1 {
				t.Errorf("%s: got %d calls, want 1", funcName, lastResult.c)
			}
			if lastResult.c > 0 && lastResult.err != nil {
				t.Errorf("%s: %s", funcName, lastResult.err)
			}
		}
	}
}
