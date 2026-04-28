package hooks

import "reflect"

// HookContext 是 hook 执行时可见的通用上下文快照。
type HookContext struct {
	RunID     string
	SessionID string
	Metadata  map[string]any
}

// Clone 返回 HookContext 的安全副本，避免元数据被跨 hook 共享修改。
func (c HookContext) Clone() HookContext {
	if len(c.Metadata) == 0 {
		return c
	}
	cloned := c
	cloned.Metadata = make(map[string]any, len(c.Metadata))
	for key, value := range c.Metadata {
		cloned.Metadata[key] = cloneMetadataValue(value)
	}
	return cloned
}

func cloneMetadataValue(value any) any {
	if value == nil {
		return nil
	}
	original := reflect.ValueOf(value)
	cloned := deepCloneValue(original)
	return cloned.Interface()
}

func deepCloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}

	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		pointer := reflect.New(value.Type().Elem())
		pointer.Elem().Set(deepCloneValue(value.Elem()))
		return pointer
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		elem := deepCloneValue(value.Elem())
		out := reflect.New(value.Type()).Elem()
		out.Set(elem)
		return out
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clonedMap := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := deepCloneValue(iter.Key())
			val := deepCloneValue(iter.Value())
			clonedMap.SetMapIndex(key, val)
		}
		return clonedMap
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		clonedSlice := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			clonedSlice.Index(i).Set(deepCloneValue(value.Index(i)))
		}
		return clonedSlice
	case reflect.Array:
		clonedArray := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			clonedArray.Index(i).Set(deepCloneValue(value.Index(i)))
		}
		return clonedArray
	default:
		return value
	}
}
