QuickSelect
===========

QuickSelect is a Go package which provides primitives for finding the smallest k elements in slices and user-defined collections. The primitives used in the package are modeled off of the standard sort library for Go. Quickselect uses [Hoare's Selection Algorithm](http://en.wikipedia.org/wiki/Selection_algorithm) which finds the smallest k elements in expected O(n) time, and is thus an asymptotically optimal algorithm (and is faster than sorting or heap implementations). QuickSelect is also faster than just Hoare's Selection Algorithm alone, since it switches to less asymptotically optimal algorithms for small k (which have smaller constant factors and thus run faster for small numbers of elements).

In addition to its main functionality of finding the smallest k elements in a slice or a user-defined collection, QuickSelect also does the following:

- Provides convenience methods for integers, floats, and strings for finding the smallest k elements without having to write boilerplate.
- Provides a reverse method to get the largest k elements in a collection.

## Example Usage

Using QuickSelect is as simple as using Go's built in sorting package, since it uses the exact same interfaces. Here's an example for getting the smallest 3 integers in an array:

```go
package quickselect_test

import (
  "fmt"
  "github.com/wangjohn/quickselect"
)

func Example_intSlice() {
  integers := []int{5, 2, 6, 3, 1, 4}
  quickselect.QuickSelect(quickselect.IntSlice(integers), 3)
  fmt.Println(integers[:3])
  // Output: [2 3 1]
}
```

For more examples, see the [documentation](https://godoc.org/github.com/wangjohn/quickselect).

## Is QuickSelect Fast?

I'm glad you asked. Actually, QuickSelect is on average about 20x faster than a naive sorting implementation. It's also faster than any selection algorithm alone, since it combines the best of many algorithms and figures out which algorithm to use for a given input.

You can see the benchmark results below (run on an Intel Core i7-4600U CPU @ 2.10GHz × 4 with 7.5 GiB of memory). Note that the name of the benchmark shows the inputs to the QuickSelect function where size denotes the length of the array and K denotes k as the integer input.

```
QuickSelect Benchmark              ns/operation   Speedup
-------------------------------------------------------------

BenchmarkQuickSelectSize1e2K1e1    27278          2.229672263
BenchmarkQuickSelectSize1e3K1e1    100185         9.038249239
BenchmarkQuickSelectSize1e3K1e2    209630         4.319501026
BenchmarkQuickSelectSize1e4K1e1    694067         17.86898815
BenchmarkQuickSelectSize1e4K1e2    1627887        7.618633849
BenchmarkQuickSelectSize1e4K1e3    2030468        6.108086904
BenchmarkQuickSelectSize1e5K1e1    6518500        31.63981913
BenchmarkQuickSelectSize1e5K1e2    8595422        23.99465215
BenchmarkQuickSelectSize1e5K1e3    16288198       12.66218405
BenchmarkQuickSelectSize1e5K1e4    20089387       10.26632425
BenchmarkQuickSelectSize1e6K1e1    67049413       30.49306274
BenchmarkQuickSelectSize1e6K1e2    70430656       29.02914829
BenchmarkQuickSelectSize1e6K1e3    110968263      18.42456484
BenchmarkQuickSelectSize1e6K1e4    161747855      12.64030337
BenchmarkQuickSelectSize1e6K1e5    189164465      10.80827711
BenchmarkQuickSelectSize1e7K1e1    689406050      32.98466508
BenchmarkQuickSelectSize1e7K1e2    684015273      33.24461976
BenchmarkQuickSelectSize1e7K1e3    737196456      30.84636053
BenchmarkQuickSelectSize1e7K1e4    1876629975     12.11737421
BenchmarkQuickSelectSize1e7K1e5    1454794314     15.6309572
BenchmarkQuickSelectSize1e7K1e6    2102171556     10.81730347
BenchmarkQuickSelectSize1e8K1e1    6822528776     39.07737829
BenchmarkQuickSelectSize1e8K1e2    6873096754     38.78987121
BenchmarkQuickSelectSize1e8K1e3    6922456224     38.51328622
BenchmarkQuickSelectSize1e8K1e4    16263664611    16.39277151
BenchmarkQuickSelectSize1e8K1e5    16568509572    16.09115996
BenchmarkQuickSelectSize1e8K1e6    20671732970    12.89715469
BenchmarkQuickSelectSize1e8K1e7    22154108390    12.03418044

Naive Sorting Benchmark            ns/operation
-------------------------------------------------------------
BenchmarkSortSize1e2K1e1           60821
BenchmarkSortSize1e3K1e1           905497
BenchmarkSortSize1e4K1e1           12402275
BenchmarkSortSize1e5K1e1           206244161
BenchmarkSortSize1e6K1e1           2044541957
BenchmarkSortSize1e7K1e1           22739827661
BenchmarkSortSize1e8K1e1           266606537874


Speedup Statistics
------------------
AVG 19.16351964
MAX 39.07737829
MIN 2.229672263
```

If you'd like to run the benchmarks yourself, you can run them by doing `go test -bench .` inside of a checkout of the quickselect source.
