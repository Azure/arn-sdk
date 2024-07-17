package msgs

import (
	"testing"

	"github.com/Azure/arn/models/v3/schema/types"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
)

func TestSubject(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want string
	}{
		{
			name: "same subscription",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000",
		},
		{
			name: "different subscription",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000001",
			want: "/",
		},
		{
			name: "same resource group",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
		},
		{
			name: "different resource group",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg1",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000",
		},
		{
			name: "same resource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
		},
		{
			name: "different resource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake1",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
		},
		{
			name: "different resource type",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources1/fake",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg",
		},
		{
			name: "same subresource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/subresource/fakesub",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/subresource/fakesub",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/subresource/fakesub",
		},
		{
			name: "different subresource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/subresource/fakesub",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/subresource/fakesub1",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
		},
		{
			name: "same extension resource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/providers/Microsoft.FakeProvider/fakeExtensionResources/fakeext",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/providers/Microsoft.FakeProvider/fakeExtensionResources/fakeext",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/providers/Microsoft.FakeProvider/fakeExtensionResources/fakeext",
		},
		{
			name: "different extension resource",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/providers/Microsoft.FakeProvider/fakeExtensionResources/fakeext",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake/providers/Microsoft.FakeProvider/fakeExtensionResources/fakeext1",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
		},
		{
			name: "different length resources",
			a:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.FakeProvider/fakeResources/fake",
			b:    "/subscriptions/00000000-0000-0000-0000-000000000000",
			want: "/subscriptions/00000000-0000-0000-0000-000000000000",
		},
	}

	for _, test := range tests {
		a, err := arm.ParseResourceID(test.a)
		if err != nil {
			t.Fatalf("TestSubject(%s): failed to parse resource ID %q: %v", test.name, test.a, err)
		}

		b, err := arm.ParseResourceID(test.b)
		if err != nil {
			t.Fatalf("TestSubject(%s): failed to parse resource ID %q: %v", test.name, test.b, err)
		}

		aarm, err := types.NewArmResource(types.ActDelete, a, "2021-10-4", nil)
		if err != nil {
			panic(err)
		}
		barm, err := types.NewArmResource(types.ActDelete, b, "2021-10-4", nil)
		if err != nil {
			panic(err)
		}

		got := subject(
			[]types.NotificationResource{
				{ArmResource: aarm},
				{ArmResource: barm},
			},
		)

		if got != test.want {
			t.Errorf("TestSubjest(%s): got %q, want %q", test.name, got, test.want)
		}
	}
}
