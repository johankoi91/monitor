#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "agora_service.h"
#include "agora_rtc_conn.h"
#include "agora_parameter.h"

// Capability experiment only: no channel is joined and no media is sent.
int main(void) {
 const char *app=getenv("RTC_NATIVE_QA_APP_ID");
 if(!app || strlen(app)!=32)return 2;
 agora_service_config cfg={0};cfg.app_id=app;cfg.enable_audio_processor=1;cfg.enable_audio_device=0;cfg.enable_video=1;cfg.area_code=0xffffffff;cfg.channel_profile=1;cfg.log_file_size_kb=1024;
 AGORA_HANDLE a=agora_service_create(); AGORA_HANDLE b=agora_service_create();
 int ia=agora_service_initialize(a,&cfg),ib=agora_service_initialize(b,&cfg);
 const char *ips[]={"111.230.192.59"};
 local_access_point_configuration ap={0};ap.ip_list=ips;ap.ip_list_size=1;ap.verify_domain_name="ap.1145326.agora.local";ap.mode=LOCAL_PROXY_MODE_LOCAL_ONLY;ap.disable_aut=true;
 rtc_conn_config cc={0};cc.client_role_type=1;cc.channel_profile=1;
 AGORA_HANDLE ca=ia==0?agora_rtc_conn_create(a,&cc):NULL;
 AGORA_HANDLE cb=ib==0?agora_rtc_conn_create(b,&cc):NULL;
 int la=ca?agora_service_set_local_access_point(a,&ap):-999;
 ap.disable_aut=false;
 int lb=cb?agora_service_set_local_access_point(b,&ap):-999;
 AGORA_HANDLE pa=ia==0?agora_service_get_agora_parameter(a):NULL;
 AGORA_HANDLE pb=ib==0?agora_service_get_agora_parameter(b):NULL;
 int sa=pa?agora_parameter_set_parameters(pa,"{\"rtc.enable_nasa2\":false,\"rtc.vos_list\":[\"111.230.192.59:4001\"]}"):-999;
 int sb=pb?agora_parameter_set_parameters(pb,"{\"rtc.enable_nasa2\":true,\"rtc.vos_list\":[\"111.230.192.59:4701\"]}"):-999;
 if(ca)agora_rtc_conn_destroy(ca);
 int ra=agora_service_release(a);
 int survivor=pb?agora_parameter_set_parameters(pb,"{\"rtc.enable_nasa2\":true}"):-999;
 if(cb)agora_rtc_conn_destroy(cb);
 int rb=agora_service_release(b);
 printf("{\"distinct_service_handles\":%s,\"initialize_a\":%d,\"initialize_b\":%d,\"connection_a\":%s,\"connection_b\":%s,\"local_ap_a\":%d,\"local_ap_b\":%d,\"parameter_handles_distinct\":%s,\"set_a\":%d,\"set_b\":%d,\"release_a\":%d,\"b_after_a_release\":%d,\"release_b\":%d}\n",a!=b?"true":"false",ia,ib,ca?"true":"false",cb?"true":"false",la,lb,pa!=pb?"true":"false",sa,sb,ra,survivor,rb);
 return ia || ib;
}
