package memory

import "fmt"

func registerMemoryHandlers(handlers []MemoryTypeHandler) (map[MemoryType]MemoryTypeHandler, []MemoryType, error) {
	types := make(map[MemoryType]MemoryTypeHandler, len(handlers))
	typeList := make([]MemoryType, 0, len(handlers))
	for _, h := range handlers {
		if h == nil {
			continue
		}
		if _, exists := types[h.Type()]; exists {
			continue
		}
		types[h.Type()] = h
		typeList = append(typeList, h.Type())
	}
	if len(typeList) == 0 {
		return nil, nil, fmt.Errorf("memory handlers are empty")
	}
	return types, typeList, nil
}
