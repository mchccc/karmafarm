package staticdata

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	_ "github.com/go-kivik/couchdb"
	"github.com/go-kivik/kivik"
	"golang.org/x/net/context"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

var CouchdbURL = getEnv("COUCHDB_URL","http://admin:pass@localhost:5984/")
var MaxGoroutine, _ = strconv.Atoi(getEnv("MAX_GOROUTINE", "20"))
var MsgQueueSize, _ = strconv.Atoi(getEnv("MSG_QUEUE_SIZE", "100"))
var InputLocation = getEnv("INPUT_LOCATION", "../../../input")
var LogLocation = getEnv("LOG_LOCATION", "../../logs")

type Crowdsourcer struct {
	Id string `json:"id"`
	Name string `json:"name"`
}

type Severity struct {
	Id string `json:"id"`
	Name string `json:"name"`
	Karma int `json:"karma"`
}

type Vulnerability struct {
	Id string `json:"id"`
	Cs *Crowdsourcer `json:"crowdsourcer"`
	Sev *Severity `json:"severity"`
}

type Finding struct {
	Id string `json:"id"`
	Time time.Time `json:"time"`
	Vulnerability *Vulnerability `json:"vulnerability"`
}

var Cs map[string]*Crowdsourcer
var Vul map[string]*Vulnerability
var Sev map[string]*Severity


func load_crowdsourcers() {
	csvFile, _ := os.Open(InputLocation + "/crowdsourcer.csv")
	reader := csv.NewReader(bufio.NewReader(csvFile))
	Cs = make(map[string]*Crowdsourcer)
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "crowdsourcer")
	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			log.Fatal(error)
		}
		Cs[line[0]] = &Crowdsourcer{
			Id: line[0],
			Name:  line[1],
		}
		csJson, _ := json.Marshal(*Cs[line[0]])
		db.Put(context.TODO(), line[0], csJson)
	}
}

func GetCrowdsourcer(cs_id string) (Crowdsourcer) {
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "crowdsourcer")
	row := db.Get(context.TODO(), cs_id)
	cs := Crowdsourcer{}
	row.ScanDoc(&cs)
	return cs
}

func load_severities() {
	csvFile, _ := os.Open(InputLocation + "/severity.csv")
	reader := csv.NewReader(bufio.NewReader(csvFile))
	Sev = make(map[string]*Severity)
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "severity")
	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			log.Fatal(error)
		}
		karma, _ := strconv.Atoi(line[2])
		Sev[line[0]] = &Severity {
			Id: line[0],
			Name:  line[1],
			Karma: karma,
		}
		sevJson, _ := json.Marshal(*Sev[line[0]])
		db.Put(context.TODO(), line[0], sevJson)
	}
}

func GetSeverity(sev_id string) (Severity) {
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "severity")
	row := db.Get(context.TODO(), sev_id)
	sev := Severity{}
	row.ScanDoc(&sev)
	return sev
}

func load_vulnerabilities() {
	csvFile, _ := os.Open(InputLocation + "/vulnerability.csv")
	reader := csv.NewReader(bufio.NewReader(csvFile))
	Vul = make(map[string]*Vulnerability)
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "vulnerability")
	for {
		line, error := reader.Read()
		if error == io.EOF {
			break
		} else if error != nil {
			log.Fatal(error)
		}
		crowdsourcer := GetCrowdsourcer(line[1])
		severity := GetSeverity(line[2])
		Vul[line[0]] = &Vulnerability{
			Id: line[0],
			Cs:  &crowdsourcer,
			Sev: &severity,
		}
		vulJson, _ := json.Marshal(*Vul[line[0]])
		db.Put(context.TODO(), line[0], vulJson)
	}
}

func GetVulnerability(vul_id string) (Vulnerability) {
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "vulnerability")
	row := db.Get(context.TODO(), vul_id)
	vul := Vulnerability{}
	row.ScanDoc(&vul)
	return vul
}

func initdb(dbname string, refreshdb bool) {
	client, _ := kivik.New("couch", CouchdbURL)
	dbexists, _ := client.DBExists(context.TODO(), dbname)
	if dbexists && refreshdb {
		client.DestroyDB(context.TODO(), dbname)
	}
	client.CreateDB(context.TODO(), dbname)
}

func add_map_and_list_views() {
	client, _ := kivik.New("couch", CouchdbURL)
	db := client.DB(context.TODO(), "finding")
	db.Put(context.TODO(), "_design/findingDesign", map[string]interface{} {
		"_id": "_design/findingDesign",
		"views": map[string]interface{}{
			"findingcs": map[string]interface{}{
				"reduce": "_sum",
				"map": "function (doc) {\n  emit([doc.vulnerability.crowdsourcer.name, doc.vulnerability.severity.name], doc.vulnerability.severity.karma);\n}",
			},
		},
	})
}

func init() {
	var wg sync.WaitGroup
	for _, dbname := range []string{"_users", "_replicator"} {
		go func(dbname string) {
			defer wg.Done()
			initdb(dbname, false)
		}(dbname)
		wg.Add(1)
	}
	wg.Wait()

	for _, dbname := range []string{"finding", "crowdsourcer", "severity", "vulnerability"} {
		go func(dbname string) {
			defer wg.Done()
			initdb(dbname, true)
		}(dbname)
		wg.Add(1)
	}
	wg.Wait()

	go func() {
		defer wg.Done()
		add_map_and_list_views()
	}()
	go func() {
		defer wg.Done()
		load_crowdsourcers()
	}()
	go func() {
		defer wg.Done()
		load_severities()
	}()
	wg.Add(3)
	wg.Wait()

	load_vulnerabilities()
}