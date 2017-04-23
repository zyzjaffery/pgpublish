package pgpublish

import (
	"database/sql"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/aws/session"
	log "github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"errors"
	"fmt"
	"encoding/base64"
	"strings"
	"strconv"
)

var (
	ErrDecodingEvent = errors.New("Error Decoding PG Event")
)

type Events2Pub struct {
	db       *sql.DB
	topicARN string
	svc      *sns.SNS
}

type Event2Publish struct {
	aggregateId string
	version int
	typecode string
	payload []byte
}

func NewEvents2Pub(db *sql.DB, topicARN string) (*Events2Pub,error) {
	var svc *sns.SNS

	if topicARN != "" {
		session, err := session.NewSession()
		if err != nil {
			log.Warnf("NewEvents2Pub: error creating session: %s", err.Error())
			return nil, err
		}

		svc = sns.New(session)
		if err := CheckTopic(svc,topicARN); err != nil {
			log.Warnf("NewEvents2Pub: error validating topic: %s", err.Error())
			return nil,err
		}
	} else {
		log.Warn("WARNING: No topic specified for NewEvents2Pub - this is only valid for certain test scenarios")
	}

	return &Events2Pub{
		db:       db,
		topicARN: topicARN,
		svc:      svc,
	}, nil
}


func CheckTopic(svc *sns.SNS, arn string) error {

	log.Println("check topic", arn)

	params := &sns.GetTopicAttributesInput{
		TopicArn: aws.String(arn),
	}

	_, err := svc.GetTopicAttributes(params)
	return err
}



func (e2p *Events2Pub) AggsWithEvents() ([]Event2Publish, error) {
	var events2Publish []Event2Publish

	rows, err := e2p.db.Query(`select aggregate_id, version, typecode, payload from es.t_aepb_publish limit 10`)
	if err != nil {
		log.Fatal(err)
	}

	var aggregateId string
	var version int
	var typecode string
	var payload []byte

	for rows.Next() {
		rows.Scan(&aggregateId,&version,&typecode,&payload)
		e2p := Event2Publish{
			aggregateId:aggregateId,
			version:version,
			typecode:typecode,
			payload:payload,
		}
		events2Publish = append(events2Publish, e2p)
	}

	err = rows.Err()

	return events2Publish, nil
}

func EncodePGEvent(aggId string, version int, payload []byte, typecode string) string {
	return fmt.Sprintf("%s:%d:%s:%s",
		aggId, version,
		base64.StdEncoding.EncodeToString(payload),
		typecode)
}

func DecodePGEvent(encoded string) (aggId string, version int, payload []byte, typecode string, err error) {
	parts := strings.Split(encoded, ":")
	if len(parts) != 4 {
		err = ErrDecodingEvent
		return
	}

	aggId = parts[0]
	version, err = strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	payload, err = base64.StdEncoding.DecodeString(parts[2])

	typecode = parts[3]

	return
}

func (e2p *Events2Pub) publishEvent(e2pub *Event2Publish) error {
	msg := EncodePGEvent(e2pub.aggregateId, e2pub.version, e2pub.payload, e2pub.version)

	params := &sns.PublishInput{
		Message:  aws.String(msg),
		Subject:  aws.String("event for aggregate " + e2pub.aggregateId),
		TopicArn: aws.String(e2p.topicARN),
	}
	resp, err := e2p.svc.Publish(params)

	log.Println(resp)
	return err
}

func (e2p *Events2Pub) PublishEvent(e2pub *Event2Publish) error {

	log.Println("Start transaction")
	tx, err := e2p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = e2p.publishEvent(e2pub)
	if err != nil {
		return nil
	}

	log.Println("delete", e2pub.aggregateId, e2pub.version)
	_, err = tx.Exec(`delete from publish where aggregate_id = $1 and version = $2`, e2pub.aggregateId, e2pub.version)
	if err != nil {
		return nil
	}

	log.Println("commit transaction")
	err = tx.Commit()
	if err != nil {
		log.Println("Error committing transaction", err.Error())
		return err
	}

	return nil
}