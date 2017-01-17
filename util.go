package main

// Helper function to create an int32 pointer
func int32p(i int32) *int32 {
	r := new(int32)
	*r = i
	return r
}
