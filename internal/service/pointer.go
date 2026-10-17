package service

// pointer returns nil with err, so a failed call never hands back a zero
// struct that reads like a real record.
func pointer[T any](v T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	return &v, nil
}
