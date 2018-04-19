package grontab

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/asdine/storm"
	"github.com/coreos/bbolt"
	"github.com/damdo/randid"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/wgliang/cron"
)

// Config defines a configuration for grontab
type Config struct {
	BucketName      string
	Persistence     bool
	PersistencePath string
}

// Job defines a job
type Job struct {
	ID      string
	Task    string
	Enabled bool
}

type jobDetails struct {
	Task    string
	Enabled bool
}

// Jobsgroup is a map of <jid(string) : task(string)>
type Jobsgroup map[string]jobDetails

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
func Init(config Config) error {
	return initialize(config)
}

// Start starts a the grontab engine
func Start() {
	start()
}

// Add adds Job to a Schedule String
func Add(schedule string, job Job) (string, error) {
	return add(job.ID, schedule, jobDetails{Task: job.Task, Enabled: job.Enabled})
}

// Remove removes a job
func Remove(id string) error {
	return remove(id)
}

// Update updates a running job
func Update(schedule string, job Job) error {
	return update(job.ID, schedule, jobDetails{Task: job.Task, Enabled: job.Enabled})
}

// List returns a list of the running schedules with their jobs
func List() map[string][]Job {
	return list()
}

// PrintJobs prints a list of the running schedules with their jobs
func PrintJobs() {
	schedules := List()
	fmt.Printf("                ID                      ENABLED            SCHEDULE           COMMAND\n")

	for gid, jobslist := range schedules {
		for _, job := range jobslist {
			fmt.Printf("%s   ["+green("%s")+"]   %s\n", job.ID, job.Enabled, gid, job.Task)
		}
	}
}

// ##################

func initialize(config Config) error {
	log.Println("Hi, this is grontab setting up")

	// setup the configuration
	grontabConfiguration = config

	var err error
	db, err = storm.Open(grontabConfiguration.PersistencePath)
	if err != nil {
		return errors.Wrap(err, "Error Initializing grontab")
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

			var jg map[string]jobDetails
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
	return nil

}

func start() {
	// startup a new cron routine
	c.Start()
}

func add(jid string, gid string, task jobDetails) (string, error) {
	// empty jobgroup to be filled
	var jg map[string]jobDetails

	err := db.Get(grontabConfiguration.BucketName, gid, &jg)
	// if err != nil means the gid schedule is new and not present in db
	// so it is necessary to create a new jg and schedule and start a new AddFunc
	if err != nil {

		// log.Printf(cyan("A new cron schedule will be created for " + gid))
		// new gid schedule, so initialize an empty jobgroup of this new gid
		jg = make(map[string]jobDetails)

		// this is a new gid, so a new schedule
		// generate a func responsible to run that gid
		worker := workerFuncGen(gid)

		// and add that func to the cron routine
		ugid := fmt.Sprintf("%s", randid.ID())
		err := c.AddFunc(gid, worker, ugid)
		if err != nil {
			return "", errors.Wrap(err, "Error Adding schedule to grontab")
		}
		ugidTable[gid] = ugid

	}

	taskAlreadyExists := false
	var taskKey string
	for k, v := range jg {
		if v.Task == task.Task || k == jid {
			taskAlreadyExists = true
			taskKey = k
			break
		}
	}

	if !taskAlreadyExists {
		// insert the job at its correspondoing jid
		// create a unique jid if not specified
		if jid == "" {
			jid = fmt.Sprintf("%s", randid.ID())
		}
		jg[jid] = jobDetails{
			Task:    task.Task,
			Enabled: task.Enabled,
		}

		// rewrite the updated jobgroup into the storage
		err := db.Set(grontabConfiguration.BucketName, gid, jg)
		if err != nil {
			// unable to add schedule in persistent storage
			return "", errors.Wrap(err, "Error Adding schedule to grontab persistent storage")
		}
		log.Printf(green("ADD JOB : {%s %s enabled:%t} to ['%s']"), jid, task.Task, task.Enabled, gid)

		return jid, nil
	}

	log.Printf("Job %s already Present at ['%s']\n", task.Task, gid)
	return taskKey, nil
}

func remove(jid string) error {
	gid, exists, err := find(jid)
	if err != nil {
		return err
	}
	if exists {
		// gets the jobgroup for that gid
		var jg map[string]jobDetails
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
			return errors.Wrap(err, "Unable to Remove job with jid: "+jid+" from grontab schedule gid "+gid)
		}

		err = garbageCollectSchedule(gid)
		if err != nil {
			return err
		}
		log.Printf(yellow("REM JOB : {%s %s enabled:%t} from ['%s']"), jid, toBeDeletedJob.Task, toBeDeletedJob.Enabled, gid)
	}
	return nil
}

func update(jid string, schedule string, task jobDetails) error {
	gid, exist, err := find(jid)
	if err != nil {
		return err
	}
	if exist {
		zjg := make(map[string]jobDetails)
		err := db.Get(grontabConfiguration.BucketName, gid, &zjg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job "+jid)
		}

		delete(zjg, jid)

		err = db.Delete(grontabConfiguration.BucketName, gid)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		err = db.Set(grontabConfiguration.BucketName, gid, zjg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		njg := make(map[string]jobDetails)
		db.Get(grontabConfiguration.BucketName, schedule, &njg)

		njg[jid] = jobDetails{
			Task:    task.Task,
			Enabled: task.Enabled,
		}

		err = db.Set(grontabConfiguration.BucketName, schedule, njg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		err = garbageCollectSchedule(gid)
		if err != nil {
			return err
		}

		// if the schedule is new start a new cron routine for it
		if schedule != gid {
			// add that func to the cron routine
			worker := workerFuncGen(schedule)
			ugid := fmt.Sprintf("%s", randid.ID())
			c.AddFunc(schedule, worker, ugid)

			// update the ugidTable
			ugidTable[schedule] = ugid
		}

		log.Printf(yellow("UPD JOB : {%s %s enabled:%t} to ['%s']"), jid, task.Task, task.Enabled, schedule)

		return nil
	}
	return errors.New(red("ERR Grontab: job.ID: '" + jid + "' non provided or doesn't exists"))
}

func list() map[string][]Job {
	jobs := make(map[string][]Job)
	keys, err := getKeys()
	if err != nil {
		log.Println("No elements in the Persistence Storage")
	} else {
		for _, gid := range keys {
			var jg map[string]jobDetails
			err := db.Get(grontabConfiguration.BucketName, gid, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + gid)
			}

			var scheduleJobs []Job
			for k, v := range jg {
				scheduleJobs = append(scheduleJobs, Job{ID: k, Task: v.Task, Enabled: v.Enabled})
			}
			jobs[gid] = scheduleJobs
		}
	}

	return jobs
}

// Generates the functions that will be executed at each cron schedule
func workerFuncGen(gid string) func() {
	// it returns a worker function
	return func() {
		jobGroupID := fmt.Sprintf("%s", randid.ID())
		log.Printf(green("RUNN JG(%s)[%s]"), jobGroupID, gid)

		var jg map[string]jobDetails

		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Putting object in storage\n")
		}

		var wg sync.WaitGroup
		// the worker func takes one job at a time from the jobgroup
		for jid, task := range jg {

			commandString := task.Task
			isTaskEnabled := task.Enabled

			if isTaskEnabled {
				log.Printf(green("EXEC JG(%s)[%s][%s]: %s"), jobGroupID, gid, jid, commandString)

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

					log.Printf(cyan("OUTP JG(%s)[%s][%s]: %s"), jobGroupID,
						gid, jid, strings.Replace(string(cmdOut), "\n", " <br> ", -1))
					wg.Done()
				}()
			}
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

func find(jid string) (string, bool, error) {
	keys, err := getKeys()
	if err != nil {
		return "", false, errors.Wrap(err, "Error finding Job")

	}

	var jg map[string]jobDetails
	for _, gid := range keys {

		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Getting object from storage for gid: " + gid)
		}
		for k := range jg {
			if jid == k {
				return gid, true, nil
			}
		}

	}
	return "", false, nil
}

// it checks if a schedule is empty and in that case delete it
func garbageCollectSchedule(gid string) error {

	// check if it is possible to garbagecollect the schedule because empty
	var jgz map[string]jobDetails
	err := db.Get(grontabConfiguration.BucketName, gid, &jgz)
	if err != nil {
		return errors.Wrap(err, "Error during garbagecollection of potentially unhused gid")
	}
	if len(jgz) == 0 {
		// the schedule is now empty from jobs, remove it from the scheduler and the persistent storage
		// stop the running schedule (gid/ugid)
		c.Remove(ugidTable[gid])
		// remove mapping from the ugidTable
		delete(ugidTable, gid)
		// remove schedule from the persistent storage
		db.Delete(grontabConfiguration.BucketName, gid)
	}
	return nil
}
