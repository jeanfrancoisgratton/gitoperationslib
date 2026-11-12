package gitoperationslib

// Ptr returns a pointer to v, for filling optional fields.
func Ptr[T any](v T) *T { return &v }
