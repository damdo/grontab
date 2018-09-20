package grontab

import (
	"fmt"
	"io/ioutil"
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

// Config defines a package configuration
type Config struct {
	BucketName         string
	PersistencePath    string
	DisableParallelism bool
	HideBanner         bool
	TurnOffLogs        bool
}

// Job defines a job
type Job struct {
	ID      string
	Task    string
	Enabled bool
}

// jobDetails define details for a job
type jobDetails struct {
	Task    string
	Enabled bool
}

// log colors
var (
	red    = color.New(color.FgRed, color.Bold).SprintFunc()
	yellow = color.New(color.FgYellow, color.Bold).SprintFunc()
	green  = color.New(color.FgGreen, color.Bold).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
)

// the package-level configuration
var grontabConfiguration Config

// the cron instance
var c *cron.Cron
var db = new(storm.DB)

// a map that keeps track of the gid and its corresponding ugid
var ugidTable = make(map[string]string)

// the banner string with the logo of the lib, to be printed in the cli
var banner string = `
                                   __             __  
   ____ _   _____  ____    ____   / /_  ____ _   / /_ 
  / __  /  / ___/ / __ \  / __ \ / __/ / __  /  / __ \
 / /_/ /  / /    / /_/ / / / / // /_  / /_/ /  / /_/ /
 \__, /  /_/     \____/ /_/ /_/ \__/  \__,_/  /_.___/ 
/____/                                                
`

// Init starts the grontab daemon and setup the persistency
func Init(config Config) error {
	return initialize(config)
}

// Start starts the grontab engine
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

// Stop stops the grontab engine
func Stop() {
	stop()
}

// ##################################
// ###### UNEXPORTED FUNCTIONS ######
// ##################################

func initialize(config Config) error {

	// setup the configuration
	grontabConfiguration = config

	// render the banner
	if !grontabConfiguration.HideBanner {
		fmt.Printf("%s\n", banner)
	}

	// if the options is enabled, turns off the logs
	if grontabConfiguration.TurnOffLogs {
		log.SetFlags(0)
		log.SetOutput(ioutil.Discard)
	}

	var err error
	// open the db connection
	db, err = storm.Open(grontabConfiguration.PersistencePath)
	if err != nil {
		return errors.Wrap(err, "Error Initializing grontab")
	}

	// create a new cron instance
	c = cron.New()

	// get keys from the storage
	keys, err2 := getKeys()
	if err2 != nil {
		log.Println("No elements in the Persistence Storage")
	} else {
		log.Println("Found Elements in the Persistence Storage, restarting them ...")

		// restart jobs from the persistent storage
		// the worker func gets the jobgroup for that gid schedule
		for _, gid := range keys {

			var jg map[string]jobDetails

			// get the tasks for the schedule
			err := db.Get(grontabConfiguration.BucketName, gid, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + gid)
			}

			// generate the worker function that executes the tasks at this schedule (gid)
			worker := workerFuncGen(gid)

			// generate a random unique id
			rid, err := randid.ID()
			if err != nil {
				return err
			}
			ugid := fmt.Sprintf("%s", rid)

			// add to the engine the worker function at this specific schetule
			err = c.AddFunc(gid, worker, ugid)
			if err != nil {
				log.Println(err)
			}

			// save the mapping gid-ugid in the table
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

	// check if the schedule is already in the db
	err := db.Get(grontabConfiguration.BucketName, gid, &jg)

	// if err != nil means the gid schedule is new and not present in db
	// so it is necessary to create a new jg and schedule and start a new AddFunc
	if err != nil {
		// new gid schedule, so initialize an empty jobgroup of this new gid
		jg = make(map[string]jobDetails)

		// this is a new gid, so a new schedule
		// generate a func responsible to run that gid
		worker := workerFuncGen(gid)

		// and add that func to the cron routine

		// generate a random unique id
		rid, err := randid.ID()
		if err != nil {
			return "", err
		}
		ugid := fmt.Sprintf("%s", rid)

		// add to the engine the worker function at this specific schetule
		err = c.AddFunc(gid, worker, ugid)
		if err != nil {
			return "", errors.Wrap(err, "Error Adding schedule to grontab")
		}

		// save the mapping gid-ugid in the table
		ugidTable[gid] = ugid

	}

	// check if the task already exists at this specific gid
	// to avoid double insertion
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
		// insert the job at its corresponding jid
		// create a unique jid if not specified
		if jid == "" {
			rid, err := randid.ID()
			if err != nil {
				return "", err
			}
			jid = fmt.Sprintf("%s", rid)
		}

		// update job details
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

	// otherwise, if the job is already present, log it, and do nothing
	log.Printf("Job %s already Present at ['%s']\n", task.Task, gid)
	return taskKey, nil
}

func remove(jid string) error {
	// find corresponding schedule id (gid) for this job id
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

		// remove a job with the specified jid from the jobgroup with specified gid
		toBeDeletedJob := jg[jid]
		delete(jg, jid)

		// rewrite the updated jobgroup into the storage
		err = db.Set(grontabConfiguration.BucketName, gid, jg)
		if err != nil {
			return errors.Wrap(err, "Unable to Remove job with jid: "+jid+" from grontab schedule gid "+gid)
		}

		// cleanup schedules that are now empty, if any
		err = garbageCollectSchedule(gid)
		if err != nil {
			return err
		}
		log.Printf(yellow("REM JOB : {%s %s enabled:%t} from ['%s']"), jid, toBeDeletedJob.Task, toBeDeletedJob.Enabled, gid)
	}
	return nil
}

func update(jid string, schedule string, task jobDetails) error {

	// find corresponding schedule id (gid) for this job id
	gid, exist, err := find(jid)
	if err != nil {
		return err
	}

	if exist {
		zjg := make(map[string]jobDetails)
		// get the jobgroup for the already existing schedule (gid)
		err := db.Get(grontabConfiguration.BucketName, gid, &zjg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job "+jid)
		}

		// delete the old jid key from the jobgroup
		delete(zjg, jid)

		// delete the jobgroup at the schedule (gid)
		err = db.Delete(grontabConfiguration.BucketName, gid)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		// set the  updated jobgroup without the old job at the schedule (gid)
		err = db.Set(grontabConfiguration.BucketName, gid, zjg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		// create a new empty jobgroup
		njg := make(map[string]jobDetails)

		// fill it with the actual jobgroup from the db
		db.Get(grontabConfiguration.BucketName, schedule, &njg)

		// update the actual jobgroup with the new task at the corresponding jid
		njg[jid] = jobDetails{
			Task:    task.Task,
			Enabled: task.Enabled,
		}

		// update the db with the new jobgroup containing the new jid with the new task
		err = db.Set(grontabConfiguration.BucketName, schedule, njg)
		if err != nil {
			return errors.Wrap(err, "Error Updating Job")
		}

		// cleanup schedules that are now empty, if any
		err = garbageCollectSchedule(gid)
		if err != nil {
			return err
		}

		// if the schedule is new start a new cron routine for it
		if schedule != gid {
			// add that func to the cron routine
			worker := workerFuncGen(schedule)

			// generate a random unique id
			rid, err := randid.ID()
			if err != nil {
				return err
			}
			ugid := fmt.Sprintf("%s", rid)

			// add to the engine the worker function at this specific schetule
			c.AddFunc(schedule, worker, ugid)

			// save the mapping gid-ugid in the table
			ugidTable[schedule] = ugid
		}

		log.Printf(yellow("UPD JOB : {%s %s enabled:%t} to ['%s']"), jid, task.Task, task.Enabled, schedule)

		// no errors return nil
		return nil
	}
	return errors.New(red("ERR Grontab: job.ID: '" + jid + "' non provided or doesn't exists"))
}

func list() map[string][]Job {

	// create an empty jobs map
	jobs := make(map[string][]Job)

	// get the keys of the schedules in the db
	keys, err := getKeys()
	if err != nil {
		log.Println("No elements in the Persistence Storage")
	} else {
		// elements are present in the storage, so some schedule exists
		// loop over the schedules (gid)
		for _, gid := range keys {

			// get the jobgroup for the corresponding schedule (gid)
			var jg map[string]jobDetails
			err := db.Get(grontabConfiguration.BucketName, gid, &jg)
			if err != nil {
				log.Panic("Error Getting object from storage for gid: " + gid)
			}

			// loop over jobs in the jobgroup and store them in the jobs map created
			var scheduleJobs []Job
			for k, v := range jg {
				scheduleJobs = append(scheduleJobs, Job{ID: k, Task: v.Task, Enabled: v.Enabled})
			}
			jobs[gid] = scheduleJobs
		}
	}

	// return the filled jobs map
	return jobs
}

// Generates the functions that will be executed at each cron schedule
func workerFuncGen(gid string) func() {
	// it returns a worker function
	return func() {

		// generate a random unique id
		rid, err := randid.ID()
		if err != nil {
			panic(err)
		}
		jobGroupID := fmt.Sprintf("%s", rid)

		log.Printf(green("RUNN JG(%s)[%s]"), jobGroupID, gid)

		// get the jobgroup for this schedule (gid)
		var jg map[string]jobDetails
		err = db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Putting object in storage\n")
		}

		var jobWaitGroup sync.WaitGroup
		var taskWaitGroup sync.WaitGroup

		// the worker func takes one job at a time from the jobgroup
		for jid, task := range jg {

			// split the task command in args ([]string)
			args := strings.Fields(task.Task)

			// if the task is enabled, proceed with executing it
			if task.Enabled {
				log.Printf(green("EXEC JG(%s)[%s][%s]: %s"), jobGroupID, gid, jid, task.Task)

				// keep count of the go routines spawned with a wait group for parallelism enabling/disabling
				jobWaitGroup.Add(1)
				if grontabConfiguration.DisableParallelism {
					taskWaitGroup.Add(1)
				}

				go func() {

					// execute the command
					cmdOut, err := exec.Command(args[0], args[1:]...).CombinedOutput()
					cleanOutput := strings.Replace(string(cmdOut), "\n", "", -1)

					if err != nil {
						log.Printf(red("Error executing: %s --> %s --> args: %#v\n"), task.Task, err, args)
					}

					log.Printf(
						cyan("OUTP JG(%s)[%s][%s]: %s"),
						jobGroupID,
						gid,
						jid,
						cleanOutput,
					)
					// keep count of the go routines spawned with a wait group for parallelism
					jobWaitGroup.Done()
					if grontabConfiguration.DisableParallelism {
						taskWaitGroup.Done()
					}
				}()

				// keep count of the go routines spawned with a wait group for parallelism
				if grontabConfiguration.DisableParallelism {
					taskWaitGroup.Wait()
				}
			}
		}
		// wait until the jobgroup is completed
		jobWaitGroup.Wait()
		log.Printf(green("ENDD JG(%s)[%s]"), jobGroupID, gid)
	}
}

func stop() {
	// stop the cron engine
	c.Stop()
	// close the storage
	db.Close()
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

// find finds an element in the db
func find(jid string) (string, bool, error) {
	// get the keys of all the schedules in the storage
	keys, err := getKeys()
	if err != nil {
		return "", false, errors.Wrap(err, "Error finding Job")

	}

	// foreach jobgroup in each schedule
	var jg map[string]jobDetails
	for _, gid := range keys {

		err := db.Get(grontabConfiguration.BucketName, gid, &jg)
		if err != nil {
			log.Panic("Error Getting object from storage for gid: " + gid)
		}
		// foreach task in the jobgroup
		for k := range jg {
			// if the current id matches with the searched one, return
			if jid == k {
				return gid, true, nil
			}
		}

	}
	return "", false, nil
}

// it checks if a schedule is empty and in that case delete it
func garbageCollectSchedule(gid string) error {
	// get the jobgroup for the schedule (gid)
	var jgz map[string]jobDetails
	err := db.Get(grontabConfiguration.BucketName, gid, &jgz)
	if err != nil {
		return errors.Wrap(err, "Error during garbage collection of potentially unused gid")
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
