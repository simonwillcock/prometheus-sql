package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
	"strings"
)

var invalidNameCharRE = regexp.MustCompile(`[^a-zA-Z0-9_]`)
var ignoreNameCharRE = regexp.MustCompile(`[%()]`)

type QueryResult struct {
	Query  *Query
	Result map[string]prometheus.Gauge // Internally we represent each facet with a JSON-encoded string for simplicity
}

// NewSetMetrics initializes a new metrics collector.
func NewQueryResult(q *Query) *QueryResult {
	r := &QueryResult{
		Query:  q,
		Result: make(map[string]prometheus.Gauge),
	}

	return r
}

func (r *QueryResult) registerMetric(facets map[string]interface{}) string {
	labels := prometheus.Labels{}

	jsonData, _ := json.Marshal(facets)
	resultKey := string(jsonData)

	for k, v := range facets {
		labels[k] = strings.ToLower(fmt.Sprintf("%v", v))
	}

	if _, ok := r.Result[resultKey]; ok { // A metric with this name is already registered
		return resultKey
	}

	fmt.Println("Registering metric", r.Query.Name, "with facets", resultKey)
	r.Result[resultKey] = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        fmt.Sprintf("query_result_%s", r.Query.Name),
		Help:        "Result of an SQL query",
		ConstLabels: labels,
	})
	prometheus.MustRegister(r.Result[resultKey])
	return resultKey
}

type record map[string]interface{}
type records []record

func setValueForResult(r prometheus.Gauge, v interface{}) error {
	switch t := v.(type) {
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return err
		}
		r.Set(f)
	case int:
		r.Set(float64(t))
	case float64:
		r.Set(t)
	default:
		return fmt.Errorf("Unhandled type %s", t)
	}
	return nil
}

func (r *QueryResult) SetMetrics(recs records) (map[string]bool, error) {
	// Queries that return only one record should only have one column
	if len(recs) > 1 && len(recs[0]) == 1 {
		return nil, errors.New("There is more than one row in the query result - with a single column")
	}

	facetsWithResult := make(map[string]bool, 0)
	if r.Query.MultiDimensional == true {
		for _, row := range recs {
			facet := make(map[string]interface{})

			if len(row) > 1 && r.Query.DataMetric {
				return nil, errors.New("Data metric not specified for multi-column query")
			}

			for k, v := range row {
				var (
					isLabel bool
				)
				for _, label := r.Query.DataLabels {
					if strings.ToLower(k) == strings.ToLower(label) {
						facet[strings.ToLower(fmt.Sprintf("%v", label))] = v
						isLabel = true;
					}
				}

				// Skip if identified as a label
				if !isLabel {
					if strings.ToLower(k) == r.Query.DataMetric {						
						// Sanitise and override name of metric to a value in the result
						r.Query.Name = ignoreNameCharRE.ReplaceAllString(v, "")
						r.Query.Name = strings.TrimSpace(invalidNameCharRE.ReplaceAllString(v, "_"))
					} else { // this is the actual gauge data
						dataVal = v
						facet[r.Query.DataLabelName] = k;
						dataFound = true
					}
				}
				
				key := r.registerMetric(facet)
				err := setValueForResult(r.Result[key], dataVal)
				if err != nil {
					return nil, err
				}
				facetsWithResult[key] = true
				
			}
		}
	} else {
		for _, row := range recs {
			facet := make(map[string]interface{})
			var (
				dataVal   interface{}
				dataFound bool
			)
			if len(row) > 1 && r.Query.DataField == "" {
				return nil, errors.New("Data field not specified for multi-column query")
			}
			for k, v := range row {
				if len(row) > 1 && strings.ToLower(k) != r.Query.DataField { // facet field, add to facets
					facet[strings.ToLower(fmt.Sprintf("%v", k))] = v
				} else { // this is the actual gauge data
					dataVal = v
					dataFound = true
				}
			}

			if !dataFound {
				return nil, errors.New("Data field not found in result set")
			}

			key := r.registerMetric(facet)
			err := setValueForResult(r.Result[key], dataVal)
			if err != nil {
				return nil, err
			}
			facetsWithResult[key] = true
		}
	}

	return facetsWithResult, nil
}

func (r *QueryResult) RemoveMissingMetrics(facetsWithResult map[string]bool) {
	for key, m := range r.Result {
		if _, ok := facetsWithResult[key]; ok {
			continue
		}
		fmt.Println("Unregistering metric", r.Query.Name, "with facets", key)
		prometheus.Unregister(m)
		delete(r.Result, key)
	}
}
