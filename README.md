##  damdo/grontab
:clock3: :arrows_counterclockwise: lib for parallel & persistent job scheduling and running, like crontab but for Go

**grontab** provides a simple way to time schedule *nix commands the same way we are all accustomed to do in crontab, thanks to the stable `robfig/cron` golang library.

It works as a normal golang package and provides persistency of the scheduled jobs across restarts thanks to a local k/v storage built with the lightning fast `coreos/bbolt`

**FEATURES**:
- **crontab syntax** : stable crontab like syntax for scheduling
- **parallel/non parallel** : jobs execution in parallel or not at the same schedule
- **persistency** : jobs persist across restarts
- **consistency** : jobs consistency and duplicate avoidance due to lightning fast ACID transactions in bbolt
- **safe and tested** : comes with high test coverage and it has been already used in production for several services and considered safe

It doesn't want to replace crontab in its day to day usefulness but it does want to provide some **advantages** in certain situations like:
integration in the language, easy to use APIs, programmble capabilities, optional job parallelism execution 

### Install

```bash
go get github.com/damdo/grontab
```

Note that the `vendor` folder is here for stability. Remove the folder if you
already have the dependencies in your GOPATH.

### Usage

#### 1) import
the first step is to import the package
```go
import(
    "github.com/damdo/grontab"
)
```

#### 2) grontab.Init()

Then grontab has to be initialized, with the Init() command, specifing the desired configuration
Boolean parameters have default value to false
The configuration lives in a `grontab.Config` that takes various parameters:

- *PersistencePath*: will be the path and name of the storage
- *BucketName*: will be the data collection name
- *DisableParallelism*: allow to choose if the jobs at the same schedule will run parallel or not
- *HideBanner*: allow to choose if the grontab banner will be shown at runtime
- *TurnOffLogs*: allow to choose if the grontab logs will be shown at runtime

Once the config is defined, it should be passed to Init() to complete the initialization

```go
newConfig := grontab.Config{
    BucketName: "jobs",
    PersistencePath: "./db.db",
    DisableParallelism: false,
    HideBanner: false,
    TurnOffLogs: false
}

err := grontab.Init(newConfig)

if err != nil {
    log.Fatal(err)
}
```

#### 3) grontab.Start()
The next step can be to spin up the grontab engine to start processing the schedule.
Start() can be invoked anytime, even after the Add()'s commands, provided that grontab as been Initialized.

```go
grontab.Start()
```

#### 4) grontab.Add()
One of the possibilities after the Init() (and optionally after Start())
is the *Add()* command. This command is meant to be used to add a new entry in grontab, a new schedule.
It takes as parameters:
1) a crontab like schedule string, [syntax here](https://godoc.org/github.com/robfig/cron#hdr-CRON_Expression_Format)
2) a `grontab.Job` which takes:
    - `Task`: a unix command `string`
    - `Enabled`: a true/false `boolean` flag to enable/disable the execution of the task

```go
newJob := grontab.Job{Task: "ping -c 4 8.8.8.8", Enabled: true}
idPing, err := grontab.Add("00 00 06 * * *", newJob)

if err != nil {
    log.Println(err)
}
```

#### 5) grontab.Update()
One of the possibilities after the Init() (and optionally after Start())
is the *Update()* command. This command is meant to be used to update already present tasks in grontab.
It takes as parameters:
1) optionally an updated crontab like schedule string, [syntax here](https://godoc.org/github.com/robfig/cron#hdr-CRON_Expression_Format)
2) a `grontab.Job` which takes:
    - `ID`: the id of the job to be updated `string` (MANDATORY)
    - `Task`: the updated unix command `string` (OPTIONALLY)
    - `Enabled`: the updated true/false `boolean` flag to enable/disable the execution of the task (OPTIONALLY)

```go
newJob := grontab.Job{Task: "ping -c 4 8.8.8.8", Enabled: true}
idPing, err := grontab.Add("00 00 06 * * *", newJob)
if err != nil {
    log.Println(err)
}

// VALID
// in this case both the schedule and the command/task of the job will be updated
err = grontab.Update("00 10 07 * 1 *", grontab.Job{ID: idPing, Task: "echo 'ciaone'", Enabled: true})
if err != nil {
	log.Println(err)
}

// VALID
// in this case the schedule of the job will be updated
err = grontab.Update("00 12 08 * 1 *", grontab.Job{ID: idPing})
if err != nil {
	log.Println(err)
}

// VALID
// in this case the schedule of the job and the Enabled status will be updated
err = grontab.Update("00 12 08 * 1 *", grontab.Job{ID: idPing, Enabled: false})
if err != nil {
	log.Println(err)
}

// INVALID
// the Update is invalid because the job ID is missing
err = grontab.Update("00 12 08 * 1 *", grontab.Job{Enabled: false})
if err != nil {
	log.Println(err)
}
```

#### 6) grontab.Remove()
One of the possibilities after the Init() (and optionally after Start())
is the *Remove()* command. This command is meant to be used to remove an entry in grontab.
It takes as parameter:
1) the id of the job to be removed

```go
newJob := grontab.Job{Task: "ping -c 4 8.8.8.8", Enabled: true}
idPing, err := grontab.Add("00 00 06 * * *", newJob)
if err != nil {
    log.Println(err)
}

err := grontab.Remove(idPing)
if err != nil {
    log.Println(err)
}
```

#### 7) grontab.Stop()
The *Stop()* command. This command is meant to be used to stop the started grontab engine.
It should be runned only after Init() and Start() have been invoked.

```go
grontab.Stop()
```

### Credits

 * [`@asdine`](https://github.com/asdine) for `github.com/asdine/storm`
 * [`@coreos`](https://github.com/coreos) for `github.com/coreos/bbolt`
 * [`@damdo`](https://github.com/damdo) for `github.com/damdo/randid`
 * [`@fatih`](https://github.com/fatih) for `github.com/fatih/color`
 * [`@davecheney`](https://github.com/davecheney) for `github.com/pkg/errors`
 * [`@robfig`](https://github.com/robfig) and [`@wgliang`](https://github.com/wgliang) for `github.com/wgliang/cron`

### License
- This project uses third party libraries that are distributed under their own terms. See [`3RD-PARTY`](https://github.com/damdo/grontab/blob/master/3RD-PARTY)
- For the rest of it the MIT License (MIT) applies. See [`LICENSE`](https://github.com/damdo/grontab/blob/master/LICENSE)  for more details
