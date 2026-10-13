openshell sandbox create --name nexus-sandbox --upload . --detach
openshell sandbox connect nexus-sandbox

#openshell provider create \
#    --name wintermute \
#    --type openai \
#    --credential OPENAI_API_KEY=empty-if-not-required \
#    --config OPENAI_BASE_URL=http://192.168.0.24:11434/v1
#
#openshell inference set \
#    --provider wintermute \
#    --model "gemma4:e4b"


openshell policy set nexus-sandbox --policy ./sandbox/policy.yml --wait

