// Diagnostic benchmarks appended through a Go overlay, never ordinary builds.
func BenchmarkApplicationProfile(b *testing.B) {
 raw, err := os.ReadFile(os.Getenv("RECEIPT_PROFILE_FIXTURE"))
 if err != nil { b.Fatal(err) }
 result, err := Decode(raw)
 if err != nil { b.Fatal(err) }
 head := result
 head.Stdout, head.Stderr = nil, nil
 metadata, err := json.Marshal(blobRecord{2, head, BlobRef{fingerprint.Hash(result.Stdout),len(result.Stdout)}, BlobRef{fingerprint.Hash(result.Stderr),len(result.Stderr)}})
 if err != nil { b.Fatal(err) }
 input, err := json.Marshal(result.Manifest.Inputs[0])
 if err != nil { b.Fatal(err) }
 b.Run("Decode",func(b *testing.B){
  b.ReportAllocs(); b.SetBytes(int64(len(raw)))
  for i:=0;i<b.N;i++ { if _,err:=Decode(raw);err!=nil {b.Fatal(err)} }
 })
 b.Run("MetadataDecode",func(b *testing.B){
  b.ReportAllocs(); b.SetBytes(int64(len(metadata)))
  for i:=0;i<b.N;i++ { var record blobRecord; if err:=json.Unmarshal(metadata,&record);err!=nil {b.Fatal(err)} }
 })
 b.Run("Encode",func(b *testing.B){
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { if _,err:=Encode(result);err!=nil {b.Fatal(err)} }
 })
 b.Run("ValidateManifest",func(b *testing.B){
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { if err:=result.Manifest.Validate();err!=nil {b.Fatal(err)} }
 })
 b.Run("ValidateResult",func(b *testing.B){
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { if err:=Validate(result);err!=nil {b.Fatal(err)} }
 })
 b.Run("ManifestKey",func(b *testing.B){
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { if result.Manifest.Key()!=result.Key {b.Fatal("key drift")} }
 })
 b.Run("InputStrict",func(b *testing.B){
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { var in fingerprint.Input; if err:=json.Unmarshal(input,&in);err!=nil {b.Fatal(err)} }
 })
 b.Run("InputPlain",func(b *testing.B){
  // Diagnostic cost floor only: this alias intentionally lacks validation.
  type plain fingerprint.Input
  b.ReportAllocs()
  for i:=0;i<b.N;i++ { var in plain; if err:=json.Unmarshal(input,&in);err!=nil {b.Fatal(err)} }
 })
}
