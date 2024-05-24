package main

import "os"
import "time"
import "net/http"
import "encoding/json"
import "fmt"
import "log"

var baseUrl = "https://portal.solaranalytics.com.au/api"

var username = os.Getenv("SA_USERNAME")
var password = os.Getenv("SA_PASSWORD")
var siteId = os.Getenv("SA_SITE_ID")

type Token struct {
	Expires   string `json:"expires"`
	ExpiresAt time.Time
	Token     string `json:"token"`
	Duration  int64  `json:"duration"`
}

type SiteData struct {
	Available bool `json:"available"`
	Data      []struct {
		Timestamp string  `json:"t_stamp"`
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
	Timeout: 5 * time.Second,
}

type SensorServer struct {
	token *Token
}

func (s *SensorServer) updateToken() error {
	if s.token != nil && s.token.ExpiresAt.After(time.Now()) {
		return nil
	}

	req, err := http.NewRequest(http.MethodGet, baseUrl+"/v3/token", nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(username, password)
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	if err = decoder.Decode(&s.token); err != nil {
		return err
	}

	if expires, err := time.Parse(time.RFC3339Nano, s.token.Expires); err == nil {
		s.token.ExpiresAt = expires
	}
	log.Println("received new token", s.token)

	return nil
}

func (s *SensorServer) updateSensors(url string, sensors interface{}) error {
	if err := s.updateToken(); err != nil {
		return fmt.Errorf("Failed to get token %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+s.token.Token)
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	decoder := json.NewDecoder(res.Body)
	if err = decoder.Decode(sensors); err != nil {
		return err
	}

	return nil
}

func (s *SensorServer) updateSiteData() (SiteData, error) {
	d := time.Now().Format("20060102")
	url := fmt.Sprintf(
		"%s/v2/site_data/%s?tstart=%s&tend=%s&all=true&gran=minute&trunc=false",
		baseUrl, siteId, d, d,
	)
	var siteData SiteData
	err := s.updateSensors(url, &siteData)
	siteData.Available = err == nil

	return siteData, err
}

func (s *SensorServer) updateLiveData() (LiveData, error) {
	var liveData LiveData
	err := s.updateSensors(
		fmt.Sprintf("%s/v3/live_site_data?site_id=%s&last_six=true", baseUrl, siteId),
		&liveData,
	)
	liveData.Available = err == nil

	return liveData, err
}

func (s *SensorServer) liveHandler(w http.ResponseWriter, r *http.Request) {
	liveData, err := s.updateLiveData()
	if err != nil {
		log.Println("Failed to update live data ", err)
		http.Error(w, fmt.Sprint("Failed to update live data ", err), http.StatusInternalServerError)
		return
	}

	data := struct {
		Available bool    `json:"available"`
		Generated float32 `json:"generated"`
		Consumed  float32 `json:"consumed"`
	}{liveData.Available, 0, 0}

	if len(liveData.Data) > 0 {
		v := liveData.Data[len(liveData.Data)-1]
		data.Generated = v.Generated
		data.Consumed = v.Consumed
	}

	json.NewEncoder(w).Encode(data)
}

func (s *SensorServer) siteHandler(w http.ResponseWriter, r *http.Request) {
	siteData, err := s.updateSiteData()
	if err != nil {
		log.Println("Failed to update live data ", err)
		http.Error(w, fmt.Sprint("Failed to update live data ", err), http.StatusInternalServerError)
		return
	}

	y, m, d := time.Now().Date()
	startTs := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	data := struct {
		Available bool      `json:"available"`
		Generated float32   `json:"generated"`
		Consumed  float32   `json:"consumed"`
		Imported  float32   `json:"imported"`
		Exported  float32   `json:"exported"`
		HotWater  float32   `json:"hot_water"`
		AC1       float32   `json:"ac1"`
		AC2       float32   `json:"ac2"`
		Stove     float32   `json:"stove"`
		Timestamp time.Time `json:"timestamp"`
	}{siteData.Available, 0, 0, 0, 0, 0, 0, 0, 0, startTs}

	for _, v := range siteData.Data {
		ts, err := time.ParseInLocation(time.DateTime, v.Timestamp, time.Local)
		if err != nil || ts.Before(startTs) {
			continue
		}

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
}

func main() {
	srv := new(SensorServer)
	http.HandleFunc("/live", srv.liveHandler)
	http.HandleFunc("/site", srv.siteHandler)
	http.ListenAndServe(":8080", nil)
}
