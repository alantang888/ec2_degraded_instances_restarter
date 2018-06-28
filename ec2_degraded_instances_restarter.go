package main

import (
	"database/sql"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"os"
	"sync"
	"time"
)

var (
	db_location   string
	db_write_lock sync.Mutex
)

const (
	EVENT_DESCRIPTION_FIELD = "event.description"
	DEGRADED_DESCRIPTION    = "The instance is running on degraded hardware"
	SLEEP_DURATION_MINUTE   = 10
	DB_LOC_VAR              = "DB_LOC"
	DB_DEFAULT_LOC          = "./instances.sqlite3"
	INSTANCE_NOT_UPDATE_SEC = 30
	DB_CREATION_SQL_STRING  = `create table processing_instance
(
  id INTEGER not null primary key autoincrement,
  create_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP not null,
  instance_id CHARACTER(19) not null unique,
  last_update_time INTEGER not null
);

create index processing_instance_instance_id_last_update_time_index
  on processing_instance (instance_id, last_update_time);`
	DB_UPDATE_PROCESSING_INSTANCE_QUERY_STRING = "INSERT OR REPLACE INTO processing_instance (instance_id, last_update_time) VALUES (?, cast(strftime('%s', 'now') as INT))"
	DB_QUERY_HANDLING_INSTANCE_QUERY_STRING    = "SELECT last_update_time FROM processing_instance WHERE instance_id=? AND cast(strftime('%s', 'now') AS INT) - last_update_time > ?"
	DB_QUERY_UNFINISHED_INSTANCE_QUERY_STRING  = "SELECT instance_id FROM processing_instance"
	DB_DELETE_PROCESSED_INSTANCE_QUERY_STRING  = "DELETE FROM processing_instance WHERE instance_id = ?"
)

func openDbConnection() *sql.DB {
	db, err := sql.Open("sqlite3", db_location)
	if err != nil {
		log.Fatalln(fmt.Sprintf("Open DB error: %v at %v.", err, db_location))
	}
	return db
}

func createTable() {
	// create instance
	db_write_lock.Lock()
	db := openDbConnection()
	defer db_write_lock.Unlock()
	defer db.Close()

	_, err := db.Exec(DB_CREATION_SQL_STRING)
	if err != nil && err.Error() != "table processing_instance already exists" {
		log.Fatalf("Create DB error: %v\n", err)
	}

}

func updateInstanceTime(instanceId *string) {
	db_write_lock.Lock()
	db := openDbConnection()
	defer db_write_lock.Unlock()
	defer db.Close()

	// update or insert
	fmt.Printf("Reach update instance status, i: %v\n", *instanceId)
	_, err := db.Exec(DB_UPDATE_PROCESSING_INSTANCE_QUERY_STRING, *instanceId)
	if err != nil {
		log.Fatalf("Update SQL error: %v\n", err)
	}
}

func isInstanceHandling(instanceId *string) bool {
	db_write_lock.Lock()
	db := openDbConnection()
	defer db_write_lock.Unlock()
	defer db.Close()

	// check is instance exist and last update less then 5 mins, if yes return handling
	rows, err := db.Query(DB_QUERY_HANDLING_INSTANCE_QUERY_STRING, *instanceId, INSTANCE_NOT_UPDATE_SEC)
	if err != nil {
		log.Fatalf("Query SQL error: %v\n", err)
	}
	if rows.Next() {
		return true
	}

	return false
}

func getPreviousSessionUnfinishTasks() *[]string {
	db_write_lock.Lock()
	db := openDbConnection()
	defer db_write_lock.Unlock()
	defer db.Close()

	rows, err := db.Query(DB_QUERY_UNFINISHED_INSTANCE_QUERY_STRING)
	if err != nil {
		log.Fatalf("Query SQL error: %v\n", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var instanceId string
		scanErr := rows.Scan(&instanceId)
		if scanErr != nil {
			log.Fatalf("Scan from row error: %v", scanErr)
		}
		result = append(result, instanceId)
	}

	return &result
}

func deleteInstance(instanceId *string) {
	db_write_lock.Lock()
	db := openDbConnection()
	defer db_write_lock.Unlock()
	defer db.Close()

	_, err := db.Exec(DB_DELETE_PROCESSED_INSTANCE_QUERY_STRING, *instanceId)
	if err != nil {
		log.Fatalf("Delete SQL entry error: %v\n", err)
	}
}

func handleDegradedInstance(instanceId *string, ec2Service *ec2.EC2, forceHandle bool) {
	// if the instance already handling. Skip it, unless it is force handle
	if !forceHandle && isInstanceHandling(instanceId) {
		return
	}

	updateInstanceTime(instanceId)

	// call ec2 to shutdown instance
	fmt.Printf("Stopping instance %v\n", *instanceId)
	targetInstance := []*string{instanceId}
	stopInstanceInput := &ec2.StopInstancesInput{InstanceIds: targetInstance}
	_, stopErr := ec2Service.StopInstances(stopInstanceInput)
	if stopErr != nil {
		log.Fatalf("Got error on stop instance %v, error: %v.\n", *instanceId, stopErr)
	}

	waitForInstanceStop(targetInstance, ec2Service, instanceId)

	// when shutdown complate call ec2 to start instance and call update instance status to update status to 0
	fmt.Printf("Starting instance %v\n", *instanceId)
	startInstanceInput := &ec2.StartInstancesInput{InstanceIds: targetInstance}
	_, startErr := ec2Service.StartInstances(startInstanceInput)
	if startErr != nil {
		log.Fatalf("Got error on start instance %v, error: %v.\n", *instanceId, startErr)
	}
	deleteInstance(instanceId)
}

func waitForInstanceStop(targetInstance []*string, ec2Service *ec2.EC2, instanceId *string) {
	// start wait for shutdown complete and start loop for every 2 minutes call update instance status to update time
	fmt.Printf("Waiting instance %v stop\n", *instanceId)
	waitStopInstanceChan := make(chan error)
	waitStopInstanceInput := &ec2.DescribeInstancesInput{InstanceIds: targetInstance}
	go func() {
		waitStopErr := ec2Service.WaitUntilInstanceStopped(waitStopInstanceInput)
		waitStopInstanceChan <- waitStopErr
	}()

waiting_loop:
	for {
		select {
		case err := <-waitStopInstanceChan:
			if err == nil {
				// instance stopped
				fmt.Printf("Instance %v stopped\n", *instanceId)
				break waiting_loop
			}
			//wait instance stop again
			fmt.Printf("Waiting 10 more minutes for instance %v stop\n", *instanceId)
			go func() {
				waitStopErr := ec2Service.WaitUntilInstanceStopped(waitStopInstanceInput)
				waitStopInstanceChan <- waitStopErr
			}()
		case <-time.After(2 * time.Minute):
			// update time record to indicate still running
			fmt.Printf("Still waiting for instance %v stop\n", *instanceId)
			updateInstanceTime(instanceId)
		}
	}
}

func getEc2Service() *ec2.EC2 {
	sess, err := session.NewSession()
	if err != nil {
		fmt.Printf("Error creating session: %v", err)
		os.Exit(1)
	}
	return ec2.New(sess)
}

func getDegradedEventFilterRequest() *ec2.DescribeInstanceStatusInput {
	// refer to https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instances-status-check_sched.html
	// to set the filter name and value
	filter := []*ec2.Filter{
		&ec2.Filter{
			Name:   aws.String(EVENT_DESCRIPTION_FIELD),
			Values: []*string{aws.String(DEGRADED_DESCRIPTION)},
		},
	}
	return &ec2.DescribeInstanceStatusInput{Filters: filter}
}

func checkDegradedResult(ec2Service *ec2.EC2, request *ec2.DescribeInstanceStatusInput) {
	degradedResult, err := ec2Service.DescribeInstanceStatus(request)
	if err != nil {
		panic(fmt.Sprintf("Error: %v", err))
	} else {
		fmt.Printf("Found %d instance(s) in degraged hardware.\n", len(degradedResult.InstanceStatuses))
		for _, instance := range degradedResult.InstanceStatuses {
			fmt.Printf("Start process instance %v\n", *instance.InstanceId)
			handleDegradedInstance(instance.InstanceId, ec2Service, false)
		}
	}
}

func main() {
	db_location = os.Getenv(DB_LOC_VAR)
	if db_location == "" {
		db_location = DB_DEFAULT_LOC
	}

	createTable()

	request := getDegradedEventFilterRequest()

	//need to handle previous unfinished tasks, assume only 1 process of this program will run
	ec2Service := getEc2Service()
	previousSessionUnfinishedTasks := getPreviousSessionUnfinishTasks()
	if len(*previousSessionUnfinishedTasks) > 0 {
		fmt.Printf("There are %d instance(s) didn't finish on previous session, processing...\n", len(*previousSessionUnfinishedTasks))
	}

	for _, instanceId := range *previousSessionUnfinishedTasks {
		go handleDegradedInstance(&instanceId, ec2Service, true)
	}

	for {
		// Get new ec2 service every loop, to prevent token timeout
		ec2Service := getEc2Service()

		fmt.Printf("Start new checking on %v\n", time.Now())
		go checkDegradedResult(ec2Service, request)
		time.Sleep(SLEEP_DURATION_MINUTE * time.Minute)
	}
}
