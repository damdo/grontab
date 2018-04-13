package grontab

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/damdo/randid"
	"github.com/wgliang/cron"

	"github.com/asdine/storm"
	"github.com/boltdb/bolt"
	"github.com/fatih/color"
)

// Config defines a configuration for grontab
type Config struct {
	BucketName      string
	Persistence     bool
	PersistencePath string
}

// Job defines a job
type Job struct {
	Jid  string
	Task string
}

// Jobsgroup is a map of <jid(string) : task(string)>
type Jobsgroup map[string]string

// log types
var red = color.New(color.FgRed, color.Bold).SprintFunc()
var yellow = color.New(color.FgYellow, color.Bold).SprintFunc()
var green = color.New(color.FgGreen, color.Bold).SprintFunc()
var cyan = color.New(color.FgCyan).SprintFunc()

// the package-level configuration
var grontabConfiguration Config

// the cron instance
var c *cron.Cron
var db = new(storm.DB)

// a map that keeps track of the gid and its corresponding ugid
var ugidTable = make(map[string]string)

// Init starts the grontab daemon and setup the persistency
func Init(config Config) {
	log.Println("Hi, this is grontab setting up")

	// setup the configuration
	grontabConfiguration = config

	var err error
	db, err = storm.Open(grontabConfiguration.PersistencePath)
	if err != nil {
		log.Panic("Error opening Db")
	}

	// create a new cron instance
	c = cron.New()

	// get keys in the storage
	keys, err2 := getKeys()
	if err2 != nil {
		log.Println("No elements in the Persistence Storage")
	} else {
		log.Println("Found Elements in the Persistence Storage, restarting them ...")

		// restart jobs from the persistent storage
		// the worker func gets the jobgroup for that gid schedule
		for _, gid := range keys {

			var jg map[string]string
			err := db.Get(grontabConfiguration.BucketName, gid, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + gid)
			}

			worker := workerFuncGen(gid)
			ugid := fmt.Sprintf("%s", randid.ID())
			err = c.AddFunc(gid, worker, ugid)
			if err != nil {
				log.Println(err)
			}
			ugidTable[gid] = ugid
		}
	}

}

// Start starts a the grontab engine
func Start() {
	// startup a new cron routine
	c.Start()
}

// Add adds Job to a Schedule String
func Add(gid string, task Job) string {

	// empty jobgroup to be filled
	var jg map[string]string

	db.Get(grontabConfiguration.BucketName, gid, &jg)

	// check if the key already exists in the db
	if _, ok := jg[gid]; !ok {
		// WARNING: keys stay there also if values

		log.Printf(cyan("New Gid Cron Schedule, a new entry will be created"))
		// new gid schedule, so initialize an empty jobgroup of this new gid
		jg = make(map[string]string)

		// this is a new gid, so a new schedule
		// generate a func responsible to run that gid
		worker := workerFuncGen(gid)

		// and add that func to the cron routine
		ugid := fmt.Sprintf("%s", randid.ID())
		err := c.AddFunc(gid, worker, ugid)
		if err != nil {
			log.Println(err)
		}
		ugidTable[gid] = ugid
	}

	taskAlreadyExists := false
	var taskKey string
	for k, v := range jg {
		if v == task.Task || k == task.Jid {
			taskAlreadyExists = true
			taskKey = k
			break
		}
	}

	if !taskAlreadyExists {
		// insert the job at its correspondoing jid

		// create a unique Jid if not specified
		if task.Jid == "" {
			task.Jid = fmt.Sprintf("%s", randid.ID())
		}
		jg[task.Jid] = task.Task

		// rewrite the updated jobgroup into the storage
		err := db.Set(grontabConfiguration.BucketName, gid, jg)
		if err != nil {
			log.Panic("Error Putting object in storage")
		}
		log.Printf(green("ADD JOB : %s to ['%s']"), task, gid)

		return task.Jid
	}
	log.Printf("Job %s already Running at ['%s']\n", task, gid)
	return taskKey
}

// Remove removes a job
func Remove(jid string) {

	gid, exists := find(jid)
	if exists {
		// gets the jobgroup for that gid
		var jg map[string]string
		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Getting object from storage")
		}

		// removes a job with the specified jid from the jobgroup with specified gid
		toBeDeletedJob := jg[jid]
		// WARNING: values are deleted but keys stay there
		delete(jg, jid)

		// rewrite the updated jobgroup into the storage
		err = db.Set(grontabConfiguration.BucketName, gid, jg)
		if err != nil {
			log.Panic("Error Putting object in storage")
		}

		// stop the running schedule (gid/ugid)
		c.Remove(ugidTable[gid])
		// renove mapping from the ugidTable
		delete(ugidTable, gid)

		log.Printf(yellow("REM JOB : {%s %s} from ['%s']"), jid, toBeDeletedJob, gid)
	}
}

// Update updates a running job
func Update(jid string, schedule string, cmd string) {

	gid, exist := find(jid)
	if exist {
		zjg := make(map[string]string)
		err := db.Get(grontabConfiguration.BucketName, gid, &zjg)
		if err != nil {
			log.Panic("Error Updating Job, Error Getting object from storage for gid: " + gid)
		}

		delete(zjg, jid)

		err = db.Delete(grontabConfiguration.BucketName, gid)
		if err != nil {
			log.Panic("Error Deleting object in storage\n")
		}

		err = db.Set(grontabConfiguration.BucketName, gid, zjg)
		if err != nil {
			log.Panic("Error Putting object in storage\n")
		}

		njg := make(map[string]string)
		db.Get(grontabConfiguration.BucketName, schedule, &njg)
		njg[jid] = cmd

		err = db.Set(grontabConfiguration.BucketName, schedule, njg)
		if err != nil {
			log.Panic("Error Putting object in storage\n")
		}

		// stop the running schedule (gid/ugid)
		c.Remove(ugidTable[gid])
		// remove mapping from the ugidTable
		delete(ugidTable, gid)

		// and add that func to the cron routine
		worker := workerFuncGen(schedule)
		ugid := fmt.Sprintf("%s", randid.ID())
		c.AddFunc(schedule, worker, ugid)
		// update the ugidTable
		ugidTable[schedule] = ugid

		log.Printf(yellow("UPDT JOB : {%s %s} to ['%s']"), jid, cmd, schedule)
	}
}

// List returns a list of the running schedules with their jobs
func List() map[string]map[string]string {

	jobs := make(map[string]map[string]string)
	keys, err := getKeys()
	if err != nil {
		log.Println("No elements in the Persistence Storage")
	} else {
		for _, gid := range keys {
			var jg map[string]string
			err := db.Get(grontabConfiguration.BucketName, gid, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + gid)
			}
			jobs[gid] = jg
		}
	}

	return jobs
}

// PrintJobs prints a list of the running schedules with their jobs
func PrintJobs() {

	schedules := List()
	fmt.Printf("                ID                      SCHEDULE           COMMAND\n")

	for gid, jobslist := range schedules {
		for k, w := range jobslist {
			fmt.Printf("%s   ["+green("%s")+"]   %s\n", k, gid, w)
		}
	}
}

// Generates the functions that will be executed at each cron schedule
func workerFuncGen(gid string) func() {
	// it returns a worker function
	return func() {
		jobGroupID := fmt.Sprintf("%s", randid.ID())
		log.Printf(green("RUNN JG(%s)[%s]"), jobGroupID, gid)

		var jg map[string]string
		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Getting object from storage")
		}

		var wg sync.WaitGroup
		// the worker func takes one job at a time from the jobgroup
		for _, commandString := range jg {
			log.Printf(green("EXEC JG(%s)[%s]: %s"), jobGroupID, gid, commandString)

			// split transform the commandstring into a actual command
			args := strings.Fields(commandString)

			// executes the command in a goroutine
			errMessage := "There was an error executing command: " + commandString + " --> "
			wg.Add(1)
			go func() {
				cmdOut, err := exec.Command(args[0], args[1:]...).Output()

				if err != nil {
					log.Printf(red(errMessage), err)
				}

				log.Printf(cyan("OUTP JG(%s)[%s]: %s"), jobGroupID,
					gid, strings.Replace(string(cmdOut), "\n", " <br> ", -1))
				wg.Done()
			}()
		}
		wg.Wait()
		log.Printf(green("ENDD JG(%s)[%s]"), jobGroupID, gid)
	}
}

// return keys of all the elements inside a bucket
func getKeys() ([]string, error) {
	var keys []string
	err := db.Bolt.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(grontabConfiguration.BucketName))
		if b == nil {
			return fmt.Errorf("No Storage bucket " + grontabConfiguration.BucketName + " found")
		}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			kstr := fmt.Sprintf("%s", k)
			// ignores the storm_metadata key
			if kstr != "__storm_metadata" {
				keys = append(keys, kstr)
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return keys, nil
}

func find(jid string) (string, bool) {
	keys, err := getKeys()
	if err != nil {
		log.Panic("Error listing keys from storage")
	}

	var jg map[string]string
	for _, gid := range keys {

		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Getting object from storage for gid: " + gid)
		}
		for k := range jg {
			if jid == k {
				return gid, true
			}
		}

	}
	return "", false
}
