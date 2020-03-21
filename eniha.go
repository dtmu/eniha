package eniha

import (
	"errors"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"time"
)

type Cluster struct {
	RouteTableId string
	CidrBlock    string
	Enis         []Eni
	gotEniIds    []bool
}

type Eni struct {
	Id        string
	CheckFunc func() error
}

var GlobalErrors []error

func (c *Cluster) FailOver(s *session.Session) map[string]string {
	svc := ec2.New(s)

	// In paralell try to get now ENI for the route (specified CIDR) and check the health of specified ENIs
	nowEniId := make(chan string, 1)
	go getNowEniIdAsync(c, svc, nowEniId)
	c.gotEniIds = make([]bool, 10)
	for p, _ := range c.Enis {
		go checkFuncAsync(c, p)
	}

	// After getting the now ENI is complete, next ENI (failover destination) try to be decided.
	// But If getting now ENI is failed, nowEniId is input "fail" to and failover is aborted.
	nowEniIdLast := <-nowEniId
	if nowEniIdLast == "fail" {
		return nil
	}

	nextEniId := make(chan string, 1)
	go stopAsync(c, nowEniIdLast, nextEniId)

	// After deciding the next ENI, ReplaceRoute (change of ENI for the route) is tyied.
	nextEniIdLast := <-nextEniId
	if nextEniIdLast == "fail" {
		return nil
	}
	input := &ec2.ReplaceRouteInput{
		DestinationCidrBlock: aws.String(c.CidrBlock),
		RouteTableId:         aws.String(c.RouteTableId),
		NetworkInterfaceId:   aws.String(nextEniIdLast),
	}
	_, err := svc.ReplaceRoute(input)
	if err != nil {
		GlobalErrors = append(GlobalErrors, errors.New("ReplaceRoute: "+err.Error()))
		return nil
	}
	return map[string]string{"before": nowEniIdLast, "after": nextEniIdLast}
}

func getNowEniIdAsync(c *Cluster, svc *ec2.EC2, nowEniId chan string) {
	input := &ec2.DescribeRouteTablesInput{
		RouteTableIds: []*string{
			aws.String(c.RouteTableId),
		},
	}
	result, err := svc.DescribeRouteTables(input)
	if err == nil {
		// Just in case, to avoid panic.
		if len(result.RouteTables) == 0 {
			GlobalErrors = append(GlobalErrors, errors.New("DescribeRouteTables: "+c.RouteTableId+" is not found."))
			nowEniId <- "fail"
			return
		}

		// If specified CIDR in the specified RouteTable is not found, Failover is failed.
		for _, route := range result.RouteTables[0].Routes {
			if aws.StringValue(route.DestinationCidrBlock) == c.CidrBlock {
				nowEniId <- aws.StringValue(route.NetworkInterfaceId)
				return
			}
		}
	}
	GlobalErrors = append(GlobalErrors, errors.New("DescribeRouteTables: "+err.Error()))
	nowEniId <- "fail"
}

func checkFuncAsync(c *Cluster, priority int) {
	if err := c.Enis[priority].CheckFunc(); err != nil {
		return
	}
	c.gotEniIds[priority] = true
}

const MAX_TRYING int = 100

func stopAsync(c *Cluster, nowEniId string, nextEniId chan string) {
	if nowEniId == "fail" {
		nextEniId <- nowEniId
		close(nextEniId)
		return
	}

	// index of now ENI is confirmed.
	var indexOfNowENI int
	for i, e := range c.Enis {
		if e.Id == nowEniId {
			indexOfNowENI = i
			break
		}
	}

	// trying once every 300 milli second for a maximum of MAX_TRYING times (that is about 30 second).
	var count int
	for {
		if count == MAX_TRYING {
			// when timeout
			nextEniId <- "fail"
			GlobalErrors = append(GlobalErrors, errors.New("OtheError: By the time limit, ENIs health could not cofirm."))
			close(nextEniId)
			return
		}
		count++
		for i, e := range c.Enis {
			if !c.gotEniIds[i] {
				continue
			}
			if i < indexOfNowENI {
				nextEniId <- e.Id
				close(nextEniId)
			}

			// The same ENI as the current one will not be set.
			if i == indexOfNowENI {
				continue
			}

			// If the now ENI is highest priority one and second priority one being health is verified, nex ENI is decided immediately.
			if indexOfNowENI == 0 && i == 1 {
				nextEniId <- e.Id
				close(nextEniId)
			}

			// After half of trying-time, The highest priority ENI at the moment is selected.
			if count == MAX_TRYING/2 {
				nextEniId <- e.Id
				close(nextEniId)
			}
		}
		time.Sleep(time.Millisecond * 300)
	}
}
