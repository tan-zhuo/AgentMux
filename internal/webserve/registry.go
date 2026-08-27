// Package webserve exposes the same services the desktop window binds over
// plain HTTP, so a browser — a tablet's, most importantly — can run the whole
// frontend against a headless AgentMux.
//
// The wire contract mirrors Wails': methods are addressed by the bound name
// the frontend already builds ("<pkg>.<Service>.<Method>"), arguments travel
// as a positional JSON array, and results come back as the method's JSON
// encoding. Keeping the contract identical is what lets one frontend serve
// both transports with a two-line switch.
package webserve

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"runtime/debug"
	"strings"
)

// Registry maps bound method names to callable service methods.
type Registry struct {
	methods map[string]reflect.Value
}

// NewRegistry indexes every exported method on the given service instances,
// keyed "<Service>.<Method>" by the concrete type's name — the same shape the
// Wails binder uses, minus the package path.
func NewRegistry(services ...any) *Registry {
	r := &Registry{methods: map[string]reflect.Value{}}
	for _, svc := range services {
		v := reflect.ValueOf(svc)
		name := v.Type().Name()
		if v.Kind() == reflect.Pointer {
			name = v.Type().Elem().Name()
		}
		for i := 0; i < v.NumMethod(); i++ {
			r.methods[name+"."+v.Type().Method(i).Name] = v.Method(i)
		}
	}
	return r
}

var errType = reflect.TypeOf((*error)(nil)).Elem()

// Call invokes a bound method with positional JSON arguments and returns the
// result marshalled to JSON. The name may carry the package-path prefix the
// frontend sends; only the final "Service.Method" is used.
func (r *Registry) Call(name string, args []json.RawMessage) (out json.RawMessage, err error) {
	// A panic in one service call fails that call and nothing else. Without
	// this the HTTP server catches it a layer up, which means the connection
	// dies rather than the request: the browser sees a network failure with no
	// sentence in it, the event stream on the same connection goes with it,
	// and the bug that caused it leaves no trace in front of the person who
	// hit it. The desktop's binder has recovered like this since it learned
	// the same lesson.
	defer func() {
		e := recover()
		if e == nil {
			return
		}
		log.Printf("panic in %s: %v\n%s", name, e, debug.Stack())
		out, err = nil, fmt.Errorf("%s failed with an internal error: %v", name, e)
	}()

	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed method name %q", name)
	}
	key := parts[len(parts)-2] + "." + parts[len(parts)-1]
	m, ok := r.methods[key]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", key)
	}

	mt := m.Type()
	if len(args) != mt.NumIn() {
		return nil, fmt.Errorf("%s takes %d arguments, got %d", key, mt.NumIn(), len(args))
	}
	in := make([]reflect.Value, len(args))
	for i, raw := range args {
		p := reflect.New(mt.In(i))
		// null stands for a zero value, the same way the Wails binder reads it.
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, p.Interface()); err != nil {
				return nil, fmt.Errorf("%s argument %d: %w", key, i, err)
			}
		}
		in[i] = p.Elem()
	}

	returned := m.Call(in)

	// The service surface uses (), (T), (error) and (T, error). Trailing error
	// first, then whatever value remains.
	if n := len(returned); n > 0 && mt.Out(n-1).Implements(errType) {
		if !returned[n-1].IsNil() {
			return nil, returned[n-1].Interface().(error)
		}
		returned = returned[:n-1]
	}
	switch len(returned) {
	case 0:
		return json.RawMessage("null"), nil
	case 1:
		b, err := json.Marshal(returned[0].Interface())
		if err != nil {
			return nil, fmt.Errorf("%s result: %w", key, err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("%s returns %d values, which the wire format cannot carry", key, len(returned))
	}
}
