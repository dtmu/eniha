# eniha
eniha is golang library for HA cluster by using ENI and Route Table in AWS.

For example, when traffic of specified CIDR go to the specified ENI, you may require HA cluster and hope failover from problem ENI to healthy ENI as below. eniha is library for that.

### When the eni-XXXXXXXXXXXXXXX is problem.
192.168.0.0/24 => eni-XXXXXXXXXXXXXXX
 |
 | failover!!
 V
192.168.0.0/24 => eni-YYYYYYYYYYYYYYY

## Usage
### 
```go
import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/dtmu/eniha"
)

func main() {
	c := eniha.Cluster{
		RouteTableId: "rtb-AAAAAAAAAAAAAAA",
		CidrBlock:    "192.168.0.0/24",
		// ENI must be specified in order of priority.
		Enis: []ec2ha.Eni{
			{
				"eni-XXXXXXXXXXXXXXX",
				// you can specified funciton for cheking the healthy fo ENI.
				// if you return error in the function, the ENI is considered unhealthy vice versa.
				func() error {return nil} 
			},
			{
				"eni-YYYYYYYYYYYYYYY",
				func() error {return nil}
			},
		},
	}
	s := session.New(&aws.Config{
		Region: aws.String("ap-northeast-1"),
	})

	// Failover is tryied. If failover is succeeded, Map[string]string as result is returned.
	// Keys are "before" and "after".
	result := c.FailOver(s) 
	if len(eniha.GlobalErrors) != 0 {
		// eniha.Globalerrors is added any output error of eniha to
		fmt.Println(eniha.GlobalErrors)
		return
	}
	fmt.Println("failover: " + result["before"] + " => " + result["after"])
}
```
