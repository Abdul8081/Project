package main

import (
	"fmt"

	cuckoo "github.com/seiflotfy/cuckoofilter"
)

func main() {
	cf := cuckoo.NewFilter(1000)
	cf.InsertUnique([]byte("geeky ogre"))

	// Lookup a string (and it a miss) if it exists in the cuckoofilter
	cf.Lookup([]byte("hello"))

	count := cf.Count()
	fmt.Println(count) // count == 1

	// Delete a string (and it a miss)
	cf.Delete([]byte("hello"))

	count = cf.Count()
	fmt.Println(count) // count == 1

	// Delete a string (a hit)
	cf.Delete([]byte("geeky ogre"))

	count = cf.Count()
	fmt.Println(count) // count == 0

	cf.Reset() // reset
}
