package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/xtracdev/pgconn"
	"github.com/xtracdev/pgpublish"
	"os"
	"time"
)

func delay() {
	time.Sleep(5 * time.Second)
}

func unlockDelay() {
	time.Sleep(1 * time.Second)
}

func publishEvents(publisher *pgpublish.EventStorePublisher) {
	log.Debug("lock table")
	gotLock, err := publisher.GetTableLock()
	if err != nil {
		log.Warnf("Error locking table: %s", err.Error())
		delay()
		return
	}

	if !gotLock {
		log.Debug("Did not obtain lock... try again in a bit")
		delay()
		return
	}

	defer func() {
		publisher.ReleaseTableLock()
		unlockDelay()
	}()

	log.Debug("get events to publish")
	events2pub, err := publisher.AggsWithEvents()
	if err != nil {
		log.Warnf("Error retrieving events to publish: %s", err.Error())
		delay()
		return
	}

	log.Debug("check events...")
	numberOfEvents := len(events2pub)
	if numberOfEvents == 0 {
		log.Debug("No events to process")
		delay()
		return
	}

	log.Debugf("Processing %d events to publish", numberOfEvents)
	for _, event := range events2pub {
		err := publisher.PublishEvent(&event)
		if err != nil {
			log.Warn("Error publishing event: %s", err.Error())
		}
	}
}




func main() {
	eventConfig, err := pgconn.NewEnvConfig()
	if err != nil {
		log.Fatal(err.Error())
	}

	pgdb, err := pgconn.OpenAndConnect(eventConfig.ConnectString(), 3)
	if err != nil {
		log.Fatal(err.Error())
	}

	topicARN := os.Getenv(pgpublish.TopicARN)
	if topicARN == "" {
		log.Fatalf("%s not specified in the environment", pgpublish.TopicARN)
	}

	if err = pgpublish.SetLogLevel(); err != nil {
		log.Warn(err.Error())
	}

	publisher, err := pgpublish.NewEvents2Pub(pgdb.DB, topicARN)
	if err != nil {
		log.Fatal(err.Error())
	}

	for {
		publishEvents(publisher)
	}

}
