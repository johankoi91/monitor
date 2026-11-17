#define _GNU_SOURCE
#include <arpa/inet.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/syscall.h>
#include <time.h>
#include <unistd.h>

// Records socket peer metadata only. No packet payload is copied or logged.
static int trace_fd = -1;
static int trace_error = 0;
static long long trace_bytes = 0;
static const char *allow_endpoints = NULL;
static pthread_mutex_t trace_lock = PTHREAD_MUTEX_INITIALIZER;
__attribute__((constructor)) static void init_trace(void) {
  allow_endpoints = getenv("RTC_PROBE_ROUTE_ALLOW_ENDPOINTS");
  const char *path = getenv("RTC_PROBE_ROUTE_LOG");
  if (path && path[0] == '/') trace_fd = open(path, O_WRONLY|O_CREAT|O_APPEND|O_CLOEXEC, 0600);
}
static void record_peer(int fd, const struct sockaddr *address, socklen_t length, ssize_t bytes, const char *direction) {
  int saved_errno = errno;
  if (trace_fd < 0 || bytes <= 0) { errno=saved_errno; return; }
  struct sockaddr_storage peer;
  if (!address || !length) {
    length=sizeof(peer);
    if (getpeername(fd,(struct sockaddr *)&peer,&length)) { errno=saved_errno; return; }
    address=(struct sockaddr *)&peer;
  }
  char ip[INET6_ADDRSTRLEN]; unsigned port;
  if (address->sa_family==AF_INET && length>=sizeof(struct sockaddr_in)) {
    const struct sockaddr_in *a=(const struct sockaddr_in *)address;
    if (!inet_ntop(AF_INET,&a->sin_addr,ip,sizeof(ip))) {errno=saved_errno;return;}
    port=ntohs(a->sin_port);
  } else if (address->sa_family==AF_INET6 && length>=sizeof(struct sockaddr_in6)) {
    const struct sockaddr_in6 *a=(const struct sockaddr_in6 *)address;
    if (!inet_ntop(AF_INET6,&a->sin6_addr,ip,sizeof(ip))) {errno=saved_errno;return;}
    port=ntohs(a->sin6_port);
  } else {errno=saved_errno;return;}
  int type; socklen_t type_len=sizeof(type);
  if (getsockopt(fd,SOL_SOCKET,SO_TYPE,&type,&type_len) || (type!=SOCK_DGRAM && type!=SOCK_STREAM)) {errno=saved_errno;return;}
  struct timespec now;clock_gettime(CLOCK_REALTIME,&now);
  char line[256];int count=snprintf(line,sizeof(line),"{\"ts_ns\":%lld,\"direction\":\"%s\",\"protocol\":\"%s\",\"ip\":\"%s\",\"port\":%u,\"bytes\":%lld,\"fd\":%d}\n",(long long)now.tv_sec*1000000000LL+now.tv_nsec,direction,type==SOCK_DGRAM?"udp":"tcp",ip,port,(long long)bytes,fd);
  if (count>0 && count<(int)sizeof(line)) {
    pthread_mutex_lock(&trace_lock);
    if(trace_bytes+count > 16*1024*1024LL) trace_error=1;
    else {ssize_t written=syscall(SYS_write,trace_fd,line,count);if(written!=count)trace_error=1;else trace_bytes+=written;}
    pthread_mutex_unlock(&trace_lock);
  }
  errno=saved_errno;
}
static int route_allowed(int fd,const struct sockaddr *address,socklen_t length) {
 if (!allow_endpoints || !*allow_endpoints) return 1;
 struct sockaddr_storage peer;
 if (!address || !length) {length=sizeof(peer);if(getpeername(fd,(struct sockaddr *)&peer,&length)) return 1;address=(struct sockaddr *)&peer;}
 char ip[INET6_ADDRSTRLEN], endpoint[INET6_ADDRSTRLEN+16];unsigned port;
 if(address->sa_family==AF_INET) {const struct sockaddr_in *a=(const struct sockaddr_in *)address;inet_ntop(AF_INET,&a->sin_addr,ip,sizeof(ip));port=ntohs(a->sin_port);snprintf(endpoint,sizeof(endpoint),"%s:%u",ip,port);}
 else if(address->sa_family==AF_INET6) {const struct sockaddr_in6 *a=(const struct sockaddr_in6 *)address;inet_ntop(AF_INET6,&a->sin6_addr,ip,sizeof(ip));port=ntohs(a->sin6_port);snprintf(endpoint,sizeof(endpoint),"[%s]:%u",ip,port);}
 else return 1;
 if(port==53) return 1;
 size_t size=strlen(endpoint);const char *p=allow_endpoints;
 while(*p){const char *end=strchr(p,';');size_t n=end?(size_t)(end-p):strlen(p);if(n==size && !strncmp(p,endpoint,n))return 1;if(!end)break;p=end+1;}
 return 0;
}
static int deny_route(int fd,const struct sockaddr *a,socklen_t l) {record_peer(fd,a,l,1,"blocked");errno=EACCES;return -1;}
#define RESOLVE(name, signature) static signature; __typeof__(real_##name) call_##name=__atomic_load_n(&real_##name,__ATOMIC_ACQUIRE); if (!call_##name) {call_##name=(__typeof__(real_##name))dlsym(RTLD_NEXT,#name);__atomic_store_n(&real_##name,call_##name,__ATOMIC_RELEASE);}
ssize_t sendto(int fd,const void *data,size_t n,int flags,const struct sockaddr *a,socklen_t l) {
  RESOLVE(sendto,ssize_t (*real_sendto)(int,const void *,size_t,int,const struct sockaddr *,socklen_t));
  if(!route_allowed(fd,a,l)){return deny_route(fd,a,l);}
  ssize_t r=call_sendto(fd,data,n,flags,a,l);record_peer(fd,a,l,r,"tx");return r;
}
ssize_t send(int fd,const void *data,size_t n,int flags) {
  RESOLVE(send,ssize_t (*real_send)(int,const void *,size_t,int));
  if(!route_allowed(fd,NULL,0)){return deny_route(fd,NULL,0);}
  ssize_t r=call_send(fd,data,n,flags);record_peer(fd,NULL,0,r,"tx");return r;
}
ssize_t recv(int fd,void *data,size_t n,int flags) {
  RESOLVE(recv,ssize_t (*real_recv)(int,void *,size_t,int));
  ssize_t r=call_recv(fd,data,n,flags);record_peer(fd,NULL,0,r,"rx");return r;
}
ssize_t recvfrom(int fd,void *data,size_t n,int flags,struct sockaddr *a,socklen_t *l) {
  RESOLVE(recvfrom,ssize_t (*real_recvfrom)(int,void *,size_t,int,struct sockaddr *,socklen_t *));
  ssize_t r=call_recvfrom(fd,data,n,flags,a,l);record_peer(fd,a,l?*l:0,r,"rx");return r;
}
ssize_t sendmsg(int fd,const struct msghdr *m,int flags) {
  RESOLVE(sendmsg,ssize_t (*real_sendmsg)(int,const struct msghdr *,int));
  if(!route_allowed(fd,m->msg_name,m->msg_namelen)){return deny_route(fd,m->msg_name,m->msg_namelen);}
  ssize_t r=call_sendmsg(fd,m,flags);record_peer(fd,m->msg_name,m->msg_namelen,r,"tx");return r;
}
ssize_t recvmsg(int fd,struct msghdr *m,int flags) {
  RESOLVE(recvmsg,ssize_t (*real_recvmsg)(int,struct msghdr *,int));
  ssize_t r=call_recvmsg(fd,m,flags);record_peer(fd,m->msg_name,m->msg_namelen,r,"rx");return r;
}
ssize_t read(int fd,void *data,size_t n) {
  RESOLVE(read,ssize_t (*real_read)(int,void *,size_t));
  ssize_t r=call_read(fd,data,n);record_peer(fd,NULL,0,r,"rx");return r;
}
ssize_t write(int fd,const void *data,size_t n) {
  RESOLVE(write,ssize_t (*real_write)(int,const void *,size_t));
  if(!route_allowed(fd,NULL,0)){return deny_route(fd,NULL,0);}
  ssize_t r=call_write(fd,data,n);record_peer(fd,NULL,0,r,"tx");return r;
}
int sendmmsg(int fd,struct mmsghdr *messages,unsigned count,int flags) {
  RESOLVE(sendmmsg,int (*real_sendmmsg)(int,struct mmsghdr *,unsigned,int));
  for(unsigned i=0;i<count;i++){if(!route_allowed(fd,messages[i].msg_hdr.msg_name,messages[i].msg_hdr.msg_namelen)){return deny_route(fd,messages[i].msg_hdr.msg_name,messages[i].msg_hdr.msg_namelen);}}
  int result=call_sendmmsg(fd,messages,count,flags);
  for(int i=0;i<result;i++) record_peer(fd,messages[i].msg_hdr.msg_name,messages[i].msg_hdr.msg_namelen,messages[i].msg_len,"tx");
  return result;
}
int recvmmsg(int fd,struct mmsghdr *messages,unsigned count,int flags,struct timespec *timeout) {
  RESOLVE(recvmmsg,int (*real_recvmmsg)(int,struct mmsghdr *,unsigned,int,struct timespec *));
  int result=call_recvmmsg(fd,messages,count,flags,timeout);
  for(int i=0;i<result;i++) record_peer(fd,messages[i].msg_hdr.msg_name,messages[i].msg_hdr.msg_namelen,messages[i].msg_len,"rx");
  return result;
}

int connect(int fd,const struct sockaddr *address,socklen_t length) {
 RESOLVE(connect,int (*real_connect)(int,const struct sockaddr *,socklen_t));
 if(!route_allowed(fd,address,length)){return deny_route(fd,address,length);}
 return call_connect(fd,address,length);
}

int rtc_route_capture_status(void) {
 pthread_mutex_lock(&trace_lock);
 int result=(trace_fd<0 || trace_error || !allow_endpoints || !*allow_endpoints)?1:0;
 pthread_mutex_unlock(&trace_lock);
 return result;
}
