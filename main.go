package main

import "os"
import "time"
import "net/http"
import "encoding/json"
import "fmt"
import "log"

var baseUrl = "https://portal.solaranalytics.com.au/api"

var username = os.Getenv("USER")
var password = os.Getenv("PASSWORD")
var siteId = os.Getenv("SITE_ID")

type Token struct {
	Expires  string `json:"expires"`
	Token    string `json:"token"`
	Duration int64  `json:"duration"`
}

type SiteData struct {
	Available bool `json:"available"`
	Data      []struct {
		Expected  float32 `json:"energy_expected"`
		Generated float32 `json:"energy_generated"`
		Consumed  float32 `json:"energy_consumed"`
		HotWater  float32 `json:"load_hot_water"`
		AC1       float32 `json:"load_other"`
		AC2       float32 `json:"load_air_conditioner"`
		Stove     float32 `json:"load_stove"`
	} `json:"data"`
}

type LiveData struct {
	Available bool `json:"available"`
	Data      []struct {
		Generated float32 `json:"generated"`
		Consumed  float32 `json:"consumed"`
	} `json:"data"`
}

var client = http.Client{
	Timeout: time.Second * 2,
}

var (
	token     *Token
	siteData  SiteData
	liveData  LiveData
	available = false
)

func updateToken() error {
	if token != nil {
		if expires, err := time.Parse(time.RFC3339Nano, token.Expires); err == nil && expires.After(time.Now()) {
			return nil
		}
	}

	req, err := http.NewRequest(http.MethodGet, baseUrl+"/v3/token", nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(username, password)
	res, getErr := client.Do(req)
	if getErr != nil {
		log.Fatal(getErr)
	}

	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	if err = decoder.Decode(&token); err != nil {
		return err
	}

	log.Println("received new token", token.Expires)

	return nil
}

func updateSensors(url string, sensors interface{}) error {
	if err := updateToken(); err != nil {
		return fmt.Errorf("Failed to get token %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+token.Token)
	res, getErr := client.Do(req)
	if getErr != nil {
		log.Fatal(getErr)
	}

	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	if err = decoder.Decode(sensors); err != nil {
		return err
	}

	return nil
}

func updateSiteData() error {
	d := time.Now().Format("20060102")
	err := updateSensors(
		fmt.Sprintf("%s/v2/site_data/%s?tstart=%s&tend=%s&all=true&gran=minute&trunc=false", baseUrl, siteId, d, d),
		&siteData,
	)
	siteData.Available = err == nil

	return err
}

func updateLiveData() error {
	err := updateSensors(
		fmt.Sprintf("%s/v3/live_site_data?site_id=%s&last_six=true", baseUrl, siteId),
		&liveData,
	)
	liveData.Available = err == nil

	return err
}

func main() {
	if err := updateLiveData(); err != nil {
		log.Fatal("Failed to update live data ", err)
	}
	if err := updateSiteData(); err != nil {
		log.Fatal("Failed to update site data ", err)
	}

	siteTicker := time.NewTicker(time.Minute)
	liveTicker := time.NewTicker(30 * time.Second)
	done := make(chan bool)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-liveTicker.C:
				if err := updateLiveData(); err != nil {
					log.Println("Failed to update live data ", err)
				}
			case <-siteTicker.C:
				if err := updateSiteData(); err != nil {
					log.Println("Failed to update site data ", err)
				}
			}
		}
	}()

	http.HandleFunc("/live", func(w http.ResponseWriter, r *http.Request) {
		data := liveData.Data[len(liveData.Data)-1]
		json.NewEncoder(w).Encode(struct {
			Available bool    `json:"available"`
			Generated float32 `json:"generated"`
			Consumed  float32 `json:"consumed"`
		}{liveData.Available, data.Generated, data.Consumed})
	})
	http.HandleFunc("/site", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Available bool    `json:"available"`
			Generated float32 `json:"generated"`
			Consumed  float32 `json:"consumed"`
			Imported  float32 `json:"imported"`
			Exported  float32 `json:"exported"`
			HotWater  float32 `json:"hot_water"`
			AC1       float32 `json:"ac1"`
			AC2       float32 `json:"ac2"`
			Stove     float32 `json:"stove"`
		}{siteData.Available, 0, 0, 0, 0, 0, 0, 0, 0}
		for _, v := range siteData.Data {
			data.Generated += v.Generated
			data.Consumed += v.Consumed
			if v.Consumed > v.Generated {
				if v.Generated > 0 {
					data.Imported += (v.Consumed - v.Generated)
				} else {
					data.Imported += v.Consumed
				}
			} else {
				data.Exported += (v.Generated - v.Consumed)
			}

			data.HotWater += v.HotWater
			data.AC1 += v.AC1
			data.AC2 += v.AC2
			data.Stove += v.Stove
		}
		json.NewEncoder(w).Encode(data)
	})
	http.ListenAndServe(":8080", nil)

	siteTicker.Stop()
	liveTicker.Stop()
	done <- true
}
