package validate

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		record  []byte
		wantErr bool
	}{
		{
			name: "valid record with all required fields",
			record: []byte(`{
				"DOI": "10.1038/test",
				"URL": "https://example.com",
				"content-domain": {"domain": []},
				"created": {"date-parts": [[2020]]},
				"deposited": {"date-parts": [[2020]]},
				"indexed": {"date-parts": [[2020]]},
				"is-referenced-by-count": 0,
				"issued": {"date-parts": [[2020]]},
				"prefix": "10.1038",
				"reference-count": 0,
				"references-count": 0,
				"resource": {"primary": {"URL": ""}},
				"score": 1.0,
				"source": "test",
				"type": "journal-article"
			}`),
			wantErr: false,
		},
		{
			name:    "invalid record missing required DOI",
			record:  []byte(`{"URL": "https://example.com"}`),
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			record:  []byte(`{not valid json`),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
