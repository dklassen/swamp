package application

import "fmt"

// Order is an order active applications list in. Each ends with the
// application ID, so it's total and stable across reloads.
type Order int

const (
	// OrderCompany: by company name ignoring case, then status in
	// workflow order, then longest at its status.
	OrderCompany Order = iota
	// OrderStatus: by status in workflow order, then company name
	// ignoring case, then longest at its status.
	OrderStatus
)

// orderNames is the single name table for Order, indexed by its value.
var orderNames = [...]string{
	OrderCompany: "company",
	OrderStatus:  "status",
}

func (o Order) String() string {
	if o < 0 || int(o) >= len(orderNames) {
		return fmt.Sprintf("Order(%d)", int(o))
	}
	return orderNames[o]
}
