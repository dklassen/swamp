package jobboard

// The company.Company.Source value each board client handles.
const (
	Ashby      = "ashby"
	Greenhouse = "greenhouse"
	Lever      = "lever"
)

// Sources is every board a company can be added from, in the order the
// company form offers them.
func Sources() []string {
	return []string{Ashby, Greenhouse, Lever}
}
