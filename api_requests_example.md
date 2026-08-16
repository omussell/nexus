<!--
# Rough notes, ignore this file
#
#This will get everything, 100 rows at a time between those two dates. The hour 12 is inclusive so its that hour 12:00 -> 12:59:59
#curl -Lv https://api.crossref.org/works\?mailto\=omussell@crossref.org\&filter=from-index-date:2025-02-02T12,until-index-date:2025-02-02T12\&rows\=100\&cursor\=\*
#
#
#cursor needs to be at the end
#setting to * will request a cursor
#thats actually an elasticsearch scroll id returned
#its a very long base64 encoded string
#it expires after 5 mins and you get a new one
#
#
#top
#{
#  "status": "ok",
#  "message-type": "work-list",
#  "message-version": "1.0.0",
#  "message": {
#    "facets": {},
#    "next-cursor": "DnF1ZXJ5VGhlbkZldGNoJAAAAAAQG0VoFnJJeVBjYXBCUlFXb0dNNkVrbFktV3cAAAAAD_UAfhZGVlRkMjlMd1NCS2RQejNad2VxaWl3AAAAAA-Xrn0WMGlXRFkzYmJTalN4STVuZ1lkRUlFQQAAAAAPwLwSFldzOW5qNjRFUVVhLW1tbzR6SWtPY0EAAAAAD_0ZfhYxcWVRRlM2bVFiZWpBenhoX2tDZWhBAAAAAA-0j-UWYUZMSVpSdWFTeUM1SEtxeVc4RWxuQQAAAAAP-VLaFngyS1ZqLWY3U0xpVnh4S1FhczdrOFEAAAAAD_UAfxZGVlRkMjlMd1NCS2RQejNad2VxaWl3AAAAAA-0j-QWYUZMSVpSdWFTeUM1SEtxeVc4RWxuQQAAAAAPl65-FjBpV0RZM2JiU2pTeEk1bmdZZEVJRUEAAAAAD-jz4RY1d1hDaEYyb1RZcVdxcW5Nclg3ZktRAAAAAA-0j-YWYUZMSVpSdWFTeUM1SEtxeVc4RWxuQQAAAAAP_Rl_FjFxZVFGUzZtUWJlakF6eGhfa0NlaEEAAAAAD-jz4hY1d1hDaEYyb1RZcVdxcW5Nclg3ZktRAAAAAA-Xrn8WMGlXRFkzYmJTalN4STVuZ1lkRUlFQQAAAAAP-VLbFngyS1ZqLWY3U0xpVnh4S1FhczdrOFEAAAAAD_UAgBZGVlRkMjlMd1NCS2RQejNad2VxaWl3AAAAAA_RmbcWdWJ6alFSd3ZSWk9jSjA4T01Ub28tQQAAAAAP-VLdFngyS1ZqLWY3U0xpVnh4S1FhczdrOFEAAAAAD_UAgRZGVlRkMjlMd1NCS2RQejNad2VxaWl3AAAAABAbRWkWckl5UGNhcEJSUVdvR002RWtsWS1XdwAAAAAP9QCCFkZWVGQyOUx3U0JLZFB6M1p3ZXFpaXcAAAAAD_0ZgBYxcWVRRlM2bVFiZWpBenhoX2tDZWhBAAAAAA-0j-cWYUZMSVpSdWFTeUM1SEtxeVc4RWxuQQAAAAAP_RmDFjFxZVFGUzZtUWJlakF6eGhfa0NlaEEAAAAAD_lS3BZ4MktWai1mN1NMaVZ4eEtRYXM3azhRAAAAAA_5Ut4WeDJLVmotZjdTTGlWeHhLUWFzN2s4UQAAAAAP-VLfFngyS1ZqLWY3U0xpVnh4S1FhczdrOFEAAAAAD_0ZgRYxcWVRRlM2bVFiZWpBenhoX2tDZWhBAAAAAA_AvBEWV3M5bmo2NEVRVWEtbW1vNHpJa09jQQAAAAAPtI_oFmFGTElaUnVhU3lDNUhLcXlXOEVsbkEAAAAAEBtFahZySXlQY2FwQlJRV29HTTZFa2xZLVd3AAAAAA_9GYIWMXFlUUZTNm1RYmVqQXp4aF9rQ2VoQQAAAAAP0Zm4FnViempRUnd2UlpPY0owOE9NVG9vLUEAAAAAD_UAgxZGVlRkMjlMd1NCS2RQejNad2VxaWl3AAAAAA-0j-kWYUZMSVpSdWFTeUM1SEtxeVc4RWxuQQ==",
#    "total-results": 686,
#    "items": [
#
#bunch of works items
#
#
#bottom
#    "items-per-page": 100,
#    "query": {
#      "start-index": 0,
#      "search-terms": null
#    }
#
#
#https://gitlab.com/crossref/data-science/snapshot-tools/-/blob/main/snapshot-updater.py?ref_type=heads
#running on a spark cluster, does multiprocessing work?
#Can we arq it instead?
-->
