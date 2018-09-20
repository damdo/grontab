package grontab

import (
	"log"
	"os"
	"reflect"
	"testing"
)

func TestListJob(t *testing.T) {
	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	grontabList := List()

	var st map[string][]Job
	if reflect.TypeOf(grontabList) != reflect.TypeOf(st) {
		t.Errorf("expected List to return a map of Jobs")
	}

}

func TestAddJob(t *testing.T) {

	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	grontabMap := List()

	if grontabMap[timing][0].ID != idPing {
		t.Errorf("expected that Add() adds or resumes the job correctly")
	}

}

func TestAddExistingJob(t *testing.T) {

	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	idPing2, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	if idPing != idPing2 {
		t.Errorf("expect Add() to avoid readd of existing timing-command job combination")
	}
}

func TestAddMultipleJobs(t *testing.T) {

	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})
	Start()

	var ids []string

	id1, err := Add("*/10 * * * * *", Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}
	ids = append(ids, id1)

	id2, err := Add("*/20 * * * * *", Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}
	ids = append(ids, id2)

	id3, err := Add("*/30 * * * * *", Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}
	ids = append(ids, id3)

	grontabMap := List()

	var actualIds []string
	for _, v := range grontabMap {
		for _, j := range v {
			actualIds = append(actualIds, j.ID)
		}
	}

	var diff []string
	m := make(map[string]bool)
	for _, item := range ids {
		m[item] = true
	}

	for _, item := range actualIds {
		if _, ok := m[item]; !ok {
			diff = append(diff, item)
		}
	}

	if len(diff) != 0 {
		t.Errorf("expected elements inserted and elements listed to be equal")
	}

}

func TestUpdateJob(t *testing.T) {

	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})
	Start()

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	otherSched := "*/30 * * * * *"
	err = Update(otherSched, Job{ID: idPing, Task: "echo 'ciaone'", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	grontabMap := List()

	if _, ok := grontabMap[otherSched]; !ok {
		t.Errorf("expected that Update() updates the time schedule correctly")
	}

	if grontabMap[otherSched][0].ID != idPing {
		t.Errorf("expected that Update() maintains the job.ID untouched")
	}

	if grontabMap[otherSched][0].Task != "echo 'ciaone'" {
		t.Errorf("expected that Update() updates the job.Task correctly")
	}

}

func TestRemoveJob(t *testing.T) {
	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	err = Remove(idPing)
	if err != nil {
		log.Println(err)
	}

	grontabMap := List()
	for _, v := range grontabMap {
		for _, j := range v {
			if j.ID == idPing {
				t.Errorf("expected that Remove() removes the job correctly")
			}
		}
	}
}

func TestRemoveNonExistingJob(t *testing.T) {
	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	err := Remove("dasdasd")
	if err == nil {
		t.Errorf("expected Remove() to raise an error trying to remove non existing job")
	}

}

func TestInitResumingJobs(t *testing.T) {

	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	Stop()

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})
	Start()

	grontabMap := List()
	if grontabMap[timing][0].ID != idPing {
		t.Errorf("expected that Add() to resume the job correctly")
	}

}

func TestWorkerFuncGen(t *testing.T) {
	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})

	Start()

	timing := "*/10 * * * * *"
	_, err := Add(timing, Job{Task: "echo 'ciaone'", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	worker := workerFuncGen(timing)
	worker()
}

func TestDisabledPallelism(t *testing.T) {
	cleaningErr := os.Remove("./db.db")
	if cleaningErr != nil && os.IsExist(cleaningErr) {
		t.Errorf("Unable to cleanup test env, before test running")
	}

	Init(Config{BucketName: "jobs", PersistencePath: "./db.db", TurnOffLogs: true, HideBanner: true})
	Start()

	// get time
	// set to next sec the schedule of both the jobs
	// give the jobs 2 sec each one
	// start a timing counter here

	timing := "*/10 * * * * *"
	idPing, err := Add(timing, Job{Task: "ping -c 4 8.8.8.8", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	otherSched := "*/10 * * * * *"
	err = Update(otherSched, Job{ID: idPing, Task: "echo 'ciaone'", Enabled: true})
	if err != nil {
		log.Println(err)
	}

	// stop a timing counter here
	// if > 4 sec, means parallelism is disabled
}
