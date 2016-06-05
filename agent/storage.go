package agent

import (
	"bytes"

	"github.com/boltdb/bolt"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/gogo/protobuf/proto"
)

// Layout:
//
//  bucket(v1.tasks.<id>) ->
//			data (task protobuf)
//			status (task status protobuf)
//			assigned (key present)
var (
	bucketKeyStorageVersion = []byte("v1")
	bucketKeyTasks          = []byte("tasks")
	bucketKeyAssigned       = []byte("assigned")
	bucketKeyData           = []byte("data")
	bucketKeyStatus         = []byte("status")
)

type bucketKeyPath [][]byte

func (bk bucketKeyPath) String() string {
	return string(bytes.Join([][]byte(bk), []byte("/")))
}

func InitDB(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		_, err := createBucketIfNotExists(tx, bucketKeyStorageVersion, bucketKeyTasks)
		return err
	})
}

func GetTask(tx *bolt.Tx, id string) (*api.Task, error) {
	var t api.Task

	if err := withTaskBucket(tx, id, func(bkt *bolt.Bucket) error {
		p := bkt.Get([]byte("data"))
		if p == nil {
			return errTaskUnknown
		}

		return proto.Unmarshal(p, &t)
	}); err != nil {
		return nil, err
	}

	return &t, nil
}

func GetTasks(tx *bolt.Tx) []*api.Task {
	bkt := getTasksBucket(tx)
	if bkt == nil {
		return nil
	}

	var tasks []*api.Task
	if err := bkt.ForEach(func(k, v []byte) error {
		tbkt := bkt.Bucket(k)

		p := tbkt.Get(bucketKeyData)
		var t api.Task
		if err := proto.Unmarshal(p, &t); err != nil {
			return err
		}

		tasks = append(tasks, &t)

		return nil
	}); err != nil {
		log.L.WithError(err).Errorf("error in GetTasks ForEach")
	}

	return tasks
}

func TaskAssigned(tx *bolt.Tx, id string) bool {
	bkt := getTaskBucket(tx, id)
	if bkt == nil {
		return false
	}

	return len(bkt.Get(bucketKeyAssigned)) > 0
}

func GetTaskStatus(tx *bolt.Tx, id string) (*api.TaskStatus, error) {
	var ts api.TaskStatus
	if err := withTaskBucket(tx, id, func(bkt *bolt.Bucket) error {
		p := bkt.Get(bucketKeyStatus)
		if p == nil {
			return errTaskUnknown
		}

		return proto.Unmarshal(p, &ts)
	}); err != nil {
		return nil, err
	}

	return &ts, nil
}

func PutTask(tx *bolt.Tx, task *api.Task) error {
	return withCreateTaskBucketIfNotExists(tx, task.ID, func(bkt *bolt.Bucket) error {
		task = task.Copy()
		task.Status = api.TaskStatus{} // blank out the status.

		p, err := proto.Marshal(task)
		if err != nil {
			return err
		}
		return bkt.Put(bucketKeyData, p)
	})
}

func PutTaskStatus(tx *bolt.Tx, id string, status *api.TaskStatus) error {
	return withCreateTaskBucketIfNotExists(tx, id, func(bkt *bolt.Bucket) error {
		p, err := proto.Marshal(status)
		if err != nil {
			return err
		}
		return bkt.Put([]byte("status"), p)
	})
}

func DeleteTask(tx *bolt.Tx, id string) error {
	bkt := getTasksBucket(tx)
	if bkt == nil {
		return nil
	}

	return bkt.DeleteBucket([]byte(id))
}

func SetTaskAssignment(tx *bolt.Tx, id string, assigned bool) error {
	return withTaskBucket(tx, id, func(bkt *bolt.Bucket) error {
		if assigned {
			return bkt.Put([]byte("assigned"), []byte{0xFF})
		} else {
			return bkt.Delete([]byte("assigned"))
		}
	})
}

func createBucketIfNotExists(tx *bolt.Tx, keys ...[]byte) (*bolt.Bucket, error) {
	log.L.Errorf("create %v", bucketKeyPath(keys))
	bkt, err := tx.CreateBucketIfNotExists(keys[0])
	if err != nil {
		return nil, err
	}

	for _, key := range keys[1:] {
		bkt, err = bkt.CreateBucketIfNotExists(key)
		if err != nil {
			return nil, err
		}
	}

	return bkt, nil
}

func withCreateTaskBucketIfNotExists(tx *bolt.Tx, id string, fn func(bkt *bolt.Bucket) error) error {
	bkt, err := createBucketIfNotExists(tx, bucketKeyStorageVersion, bucketKeyTasks, []byte(id))
	if err != nil {
		return err
	}

	return fn(bkt)
}

func withTaskBucket(tx *bolt.Tx, id string, fn func(bkt *bolt.Bucket) error) error {
	bkt := getTaskBucket(tx, id)
	if bkt == nil {
		return errTaskUnknown
	}

	return fn(bkt)
}

func getTaskBucket(tx *bolt.Tx, id string) *bolt.Bucket {
	return getBucket(tx, bucketKeyStorageVersion, bucketKeyTasks, []byte(id))
}

func getTasksBucket(tx *bolt.Tx) *bolt.Bucket {
	return getBucket(tx, bucketKeyStorageVersion, bucketKeyTasks)
}

func getBucket(tx *bolt.Tx, keys ...[]byte) *bolt.Bucket {
	log.L.Debugf("getBucket %v", bucketKeyPath(keys))
	bkt := tx.Bucket(keys[0])

	for _, key := range keys[1:] {
		if bkt == nil {

			log.L.Debugf("getBucket %v, missing at %v", bucketKeyPath(keys), string(key))
			break
		}
		bkt = bkt.Bucket(key)
	}

	return bkt
}
