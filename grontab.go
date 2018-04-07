package grontab

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/asdine/storm"
	"github.com/boltdb/bolt"
	"github.com/fatih/color"
	"github.com/robfig/cron"
	uuid "github.com/satori/go.uuid"
)

// Job defines a job
type Job struct {
	Jid  string
	Task string
}

// Config defines a configuration for grontab
type Config struct {
	BucketName      string
	Persistence     bool
	PersistencePath string
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
		for _, v := range keys {

			var jg map[string]string
			err := db.Get(grontabConfiguration.BucketName, v, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + v)
			}

			worker := workerFuncGen(v)
			c.AddFunc(v, worker)
		}
	}

}

// Start starts a the grontab engine
func Start() {
	// startup a new cron routine
	go c.Start()
}

// Add adds Job to a Schedule String
func Add(gid string, task Job) string {

	// empty jobgroup to be filled
	var jg map[string]string

	err := db.Get(grontabConfiguration.BucketName, gid, &jg)
	if err != nil {
		log.Printf(cyan("New Gid Cron Schedule, a new entry will be created"))
		// new gid schedule, so initialize an empty jobgroup of this new gid
		jg = make(map[string]string)

		// this is a new gid, so a new schedule
		// generate a func responsible to run that gid
		worker := workerFuncGen(gid)

		// and add that func to the cron routine
		c.AddFunc(gid, worker)
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
			task.Jid = fmt.Sprintf("%s", uuid.NewV4())
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

// Remove removes a job inside a specific Schedule String (aka jobgroup)
func Remove(gid string, jid string) {

	// gets the jobgroup for that gid
	var jg map[string]string
	err := db.Get(grontabConfiguration.BucketName, gid, &jg)
	if err != nil {
		log.Panic("Error Getting object from storage")
	}

	// removes a job with the specified jid from the jobgroup with specified gid
	toBeDeletedJob := jg[jid]
	delete(jg, jid)

	// rewrite the updated jobgroup into the storage
	err = db.Set(grontabConfiguration.BucketName, gid, jg)
	if err != nil {
		log.Panic("Error Putting object in storage")
	}
	log.Printf(yellow("REM JOB : {%s %s} from ['%s']"), jid, toBeDeletedJob, gid)
}

// ###### FUNCTIONS ######

func workerFuncGen(gid string) func() {
	// it returns a worker function
	return func() {
		jobGroupID := fmt.Sprintf("%s", uuid.NewV4())
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
