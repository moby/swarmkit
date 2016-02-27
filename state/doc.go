// Package state provides interfaces to work with swarm cluster state.
//
// The primary interface is Store, which abstracts storage of this cluster
// state. Store exposes a transactional interface for both reads and writes.
// To begin a read, BeginRead() returns a ReadTx object that exposes the state
// in a consistent way. Similarly, Begin() returns a Tx object that allows
// reads and writes to happen without interference from other transactions.
// Either type of transaction must be finished with the Close method.
//
// This is an example of making an update to a Store:
//
//	tx, err := store.Begin()
//	if err != nil {
//		return err
//	}
//	defer tx.Close()
//	if err := tx.Nodes().Update(newNode); err != nil {
//		reutrn err
//	}
//
// WatchableStore is a version of Store that exposes watch functionality.
// These expose a publish/subscribe queue where code can subscribe to
// changes of interest. This can be combined with the Snapshot method to
// "fork" a store, by making a snapshot and then applying future changes
// to keep the copy in sync. This approach lets consumers of the data
// use their own data structures and implement their own concurrency
// strategies. It can lead to more efficient code because data consumers
// don't necessarily have to lock the main data store if they are
// maintaining their own copies of the state.
package state
