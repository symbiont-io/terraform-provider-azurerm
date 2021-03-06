package suppress

import (
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
)

func CaseDifference(_, old, new string, _ *schema.ResourceData) bool {
	return strings.ToLower(old) == strings.ToLower(new)
}
