# hexo-blog-satoken
重构博客

这里先简单的介绍一下每一个接口的实现，后面通过swagger来md导出，再将这些加进去。

图形人机验证，这个是暂时使用的原博主的CaptchaAppId=2088053498，具体可以去这里https://main.qcloudimg.com/raw/document/product/pdf/1110_44904_cn.pdf

注意完成开发后要把邮箱验证功能恢复

Sa-token的Session：

> Sa-Token Session可以理解为 HttpSession 的升级版：
>
> 1. Sa-Token只在调用`StpUtil.login(id)`登录会话时才会产生Session，不会为每个陌生会话都产生Session，节省性能
> 2. 在登录时产生的Session，是分配给账号id的，而不是分配给指定客户端的，也就是说在PC、APP上登录的同一账号所得到的Session也是同一个，所以两端可以非常轻松的同步数据
> 3. Sa-Token支持Cookie、Header、body三个途径提交Token，而不是仅限于Cookie
> 4. 由于不强依赖Cookie，所以只要将Token存储到不同的地方，便可以做到一个客户端同时登录多个账号

注意搜索一下readme文档中的todo





## 自定义架构模块

### 角色权限管理模块

#### 路由管理

通过SaTokenConfigure类：

```java
@Configuration
@Slf4j
public class SaTokenConfigure implements WebMvcConfigurer {


    @Resource
    HttpServletRequest request;

    @Autowired
    MySourceSafilterAuthStrategy mySourceSafilterAuthStrategy;


    /**
     * 注册 [Sa-Token 拦截器]
     * DispatcherServlet 之后
     */
    @Override
    public void addInterceptors(InterceptorRegistry registry) {
        // 注册路由拦截器，自定义认证规则
        registry.addInterceptor(new SaInterceptor(handler -> {
            // 登录校验 -- 拦截所有路由，并排除/user/doLogin 用于开放登录
            SaRouter.match("/admin/**", "/login", r -> StpUtil.checkLogin());
            // 甚至你可以随意的写一个打印语句
            SaRouter.match("/**", r -> log.info("----啦啦啦跑了一个匿名接口了----路径为：" + request.getRequestURI()));
        })).addPathPatterns("/**");
    }

    /**
     * 注册 [Sa-Token 全局过滤器]
     * DispatcherServlet 之前
     */
    @Bean
    public SaServletFilter getSaServletFilter() {
        return new SaServletFilter()

                // 指定 [拦截路由] 与 [放行路由]
                .addInclude("/admin/**")
                .addExclude("/favicon.ico")
                .addExclude("/login")
                .addExclude("/doc.html")

                // 认证函数: 每次请求执行
                .setAuth(mySourceSafilterAuthStrategy)
                // 异常处理函数：每次认证函数发生异常时执行此函数
                .setError(e -> {
                    // 2022/11/18 这里就是用户权限不足的时候
                    log.info(e.getMessage());
                    return SaResult.error(e.getMessage());
                })

                // 前置函数：在每次认证函数之前执行
                .setBeforeAuth(r -> {
                    // ---------- 设置一些安全响应头 ----------
                    SaHolder.getResponse()
                            // 服务器名称
                            .setServer("sa-server")
                            // 是否可以在iframe显示视图： DENY=不可以 | SAMEORIGIN=同域下可以 | ALLOW-FROM uri=指定域名下可以
                            .setHeader("X-Frame-Options", "SAMEORIGIN")
                            // 是否启用浏览器默认XSS防护： 0=禁用 | 1=启用 | 1; mode=block 启用, 并在检查到XSS攻击时，停止渲染页面
                            .setHeader("X-XSS-Protection", "1; mode=block")
                            // 禁用浏览器内容嗅探
                            .setHeader("X-Content-Type-Options", "nosniff")
                    ;
                })
                ;
    }

}
```

全局过滤器中用了一下自定义的`MySourceSafilterAuthStrategy`资源角色管理办法

```java
@Component
@Slf4j
public class MySourceSafilterAuthStrategy implements SaFilterAuthStrategy {

    /**
     * 资源角色列表
     */
    private static List<ResourceRoleDTO> resourceRoleList;


    @Autowired
    private RoleMapper roleDao;

    @Resource
    private HttpServletRequest request;


    /**
     * 加载资源角色信息
     */
    @PostConstruct // 在Spring中：Constructor >> @Autowired >> @PostConstruct
    private void loadDataSource() {
        resourceRoleList = roleDao.listResourceRoles();
        log.info("resourceRoleList = " + resourceRoleList);
    }

    /**
     * 清空接口角色信息
     */
    public void clearDataSource() {
        resourceRoleList = null;
    }

    /**
     * 作者： ladidol
     * 描述： 这里判断权限是不是够。
     */
    @Override
    public void run(Object r) {

        try {
            // 修改接口角色关系后重新加载
            if (CollectionUtils.isEmpty(resourceRoleList)) {
                this.loadDataSource();
            }
            // 获取用户请求方式
            String method = request.getMethod();
            // 获取用户请求Url
            String url = request.getRequestURI();
            // 获取用户的角色
            List<String> curUserRoleList = StpUtil.getRoleList();

            log.info("method = " + method);
            log.info("url = " + url);
            log.info("curUserRoleList = " + curUserRoleList);


            AntPathMatcher antPathMatcher = new AntPathMatcher();
            // 获取接口角色信息，若为匿名接口则放行，若无对应角色则禁止
            for (ResourceRoleDTO resourceRoleDTO : resourceRoleList) {
                if (antPathMatcher.match(resourceRoleDTO.getUrl(), url) && resourceRoleDTO.getRequestMethod().equals(method)) {
                    List<String> roleList = resourceRoleDTO.getRoleList();
                    if (CollectionUtils.isEmpty(roleList)) {
                        // 2022/11/18 不出所料这里是匿名放行
                        log.info("匿名接口放行");
                        return;
                    }
                    // 2022/11/18 找到了这个接口所需要的角色信息
                    for (String role : roleList) {
                        if (curUserRoleList.contains(role)) {
                            log.info("用户通过了其中一个角色验证：" + role);
                            return;//说明放行
                        }
                    }
                    throw new AppException("用户：" + StpUtil.getLoginId() + " 权限不够；url：" + url);

                }
            }
        } catch (NullPointerException e) {
            throw new AppException("发送空指针异常");
        }
        log.warn("这个接口没有被数据库记录！");

    }
}
```

每一个接口的访问权限or角色信息`resourceRoleList`在后端启动之时就随着类的实例化托管于Spring容器中了，每一次有被管理的请求发生都会走这个run方法，进行鉴权判断。

#### 角色管理

每一次接口的角色信息更新都会调一下`clearDataSource`清空`resourceRoleList`中的数据，同时下次接口访问的时候会调用`loadDataSource`将`resourceRoleList`重新加载

### 限流注解拦截器

#### 准备

1. 自定一个redis接口限流注解

   ```java
   @Target(ElementType.METHOD)
   @Retention(RetentionPolicy.RUNTIME)
   @Documented
   public @interface AccessLimit {
   
       /**
        * 单位时间（秒）
        *
        * @return int
        */
       int seconds();
   
       /**
        * 单位时间最大请求次数
        *
        * @return int
        */
       int maxCount();
   }
   
   ```

2. 标记在你想要限流的接口上

   ```java
   @AccessLimit(seconds = 6, maxCount = 1)
   @ApiOperation(value = "发送邮箱验证码")
   @ApiImplicitParam(name = "username", value = "用户名", required = true, dataType = "String")
   @GetMapping("/users/code")
   public Result<?> sendCode(String username) {
       userAuthService.sendCode(username);
       return Result.ok();
   }
   ```

#### 拦截

1. 自定义限流拦截器`AccessLimitHandler`

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 12:25
    * @description: 限流拦截器
    */
   @Log4j2
   public class AccessLimitHandler implements HandlerInterceptor {
       @Autowired
       private RedisService redisService;
   
       @Override
       public boolean preHandle(HttpServletRequest httpServletRequest, HttpServletResponse httpServletResponse, Object handler) throws Exception {
           // 如果请求输入方法
           if (handler instanceof HandlerMethod) {
               HandlerMethod hm = (HandlerMethod) handler;
               // 获取方法中的注解,看是否有该注解
               AccessLimit accessLimit = hm.getMethodAnnotation(AccessLimit.class);
               if (accessLimit != null) {
                   long seconds = accessLimit.seconds();
                   int maxCount = accessLimit.maxCount();
                   // 关于key的生成规则可以自己定义 本项目需求是对每个方法都加上限流功能，如果你只是针对ip地址限流，那么key只需要只用ip就好
                   String key = IpUtils.getIpAddress(httpServletRequest) + hm.getMethod().getName();
                   // 从redis中获取用户访问的次数
                   try {
                       // 此操作代表获取该key对应的值自增1后的结果
                       long q = redisService.incrExpire(key, seconds);
                       if (q > maxCount) {
                           render(httpServletResponse, Result.fail("请求过于频繁，请稍候再试"));
                           log.warn(key + "请求次数超过每" + seconds + "秒" + maxCount + "次");
                           return false;
                       }
                       return true;
                   } catch (RedisConnectionFailureException e) {
                       log.warn("redis错误: " + e.getMessage());
                       return false;
                   }
               }
           }
           return true;
       }
   
       /**
        * 作者：Ladidol
        * 描述：将不通过的结果返回给前端
        */
       private void render(HttpServletResponse response, Result<?> result) throws Exception {
           response.setContentType(APPLICATION_JSON);
           OutputStream out = response.getOutputStream();
           String str = JSON.toJSONString(result);
           out.write(str.getBytes(StandardCharsets.UTF_8));
           out.flush();
           out.close();
       }
   
   }
   ```

2. 在`WebMvcConfigurer`的实现类中注册拦截器

   ```java
   /** <p>
       * 作者：Ladidol
       * 描述：WebMvc注册拦截器
       */
       @Override
       public void addInterceptors(InterceptorRegistry registry) {
   
       //注册限流拦截器
       registry.addInterceptor(getAccessLimitHandler());
   }
   ```

### 分页拦截器

#### 准备

1. 准备一个分页工具类

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 12:49
    * @description: 分页工具类
    */
   public class PageUtils {
   
       //ThreadLocal，线程的局部变量，只有当前线程能访问，这里就表示只有当前请求才能访问的
       private static final ThreadLocal<Page<?>> PAGE_HOLDER = new ThreadLocal<>();
   
       public static void setCurrentPage(Page<?> page) {
           PAGE_HOLDER.set(page);
       }
   
       public static Page<?> getPage() {
           Page<?> page = PAGE_HOLDER.get();
           if (Objects.isNull(page)) {
               setCurrentPage(new Page<>());
           }
           return PAGE_HOLDER.get();
       }
   
       public static Long getCurrent() {
           return getPage().getCurrent();
       }
   
       public static Long getSize() {
           return getPage().getSize();
       }
   
       public static Long getLimitCurrent() {
           return (getCurrent() - 1) * getSize();
       }
   
       public static void remove() {
           PAGE_HOLDER.remove();
       }
   
   }
   ```

2. 值得注意的是，这里的用`ThreadLocal`来保存页面变量，使用范围只是当前线程：这里就是当前请求这段时间

   ```java
   //ThreadLocal，线程的局部变量，只有当前线程能访问，这里就表示只有当前请求才能访问的
   private static final ThreadLocal<Page<?>> PAGE_HOLDER = new ThreadLocal<>();
   ```

3. 然后在准备一个`PageResult.java`，用于给前端返回当前页的数据，和当前页的个数，可以拓展，看前端具体要什么。

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 13:15
    * @description: 分页对象
    */
   @Data
   @AllArgsConstructor
   @NoArgsConstructor
   @Builder
   @ApiModel(description = "分页对象")
   public class PageResult<T> {
   
       /**
        * 分页列表
        */
       @ApiModelProperty(name = "recordList", value = "分页列表", required = true, dataType = "List<T>")
       private List<T> recordList;
   
       /**
        * 总数
        */
       @ApiModelProperty(name = "count", value = "总数", required = true, dataType = "Integer")
       private Integer count;
   
   }
   
   ```

   



#### 拦截

1. 主要通过拦截看一下前端有没有传参数current和size，这两个表示要分页，这时候就新建一个页面等待分页。所以说拦截器的主要功能就是定义页面大小和当前页`setCurrentPage`，存入`PageUtils`中的局部变量中去，供工具类`PageUtils`使用。

2. 先实现一个拦截器：

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 12:46
    * @description: 分页拦截器，根据请求处理是否分页
    */
   public class PageableHandlerInterceptor implements HandlerInterceptor {
   
       @Override
       public boolean preHandle(HttpServletRequest request, HttpServletResponse response, Object handler) {
           String currentPage = request.getParameter(CURRENT);
           String pageSize = Optional.ofNullable(request.getParameter(SIZE)).orElse(DEFAULT_SIZE);
           if (!StringUtils.isNullOrEmpty(currentPage)) {
               PageUtils.setCurrentPage(new Page<>(Long.parseLong(currentPage), Long.parseLong(pageSize)));
           }
           return true;
       }
   
       @Override
       public void afterCompletion(HttpServletRequest request, HttpServletResponse response, Object handler, Exception ex) {
           PageUtils.remove();
       }
   
   }
   ```

3. 注册拦截器：

   ```java
   /**
        * <p>
        * 作者：Ladidol
        * 描述：WebMvc注册拦截器
        */
   @Override
   public void addInterceptors(InterceptorRegistry registry) {
   
       //注册分页拦截器
       registry.addInterceptor(new PageableHandlerInterceptor());
   
   }
   ```

   

### 自定义操作日记注解

#### 准备

1. 自定一个注解`@OptLog`

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 15:00
    * @description: 操作日志注解
    */
   @Target(ElementType.METHOD)
   @Retention(RetentionPolicy.RUNTIME)
   @Documented
   public @interface OptLog {
   
       /**
        * @return 操作类型
        */
       String optType() default "";
   
   }
   ```

2. 操作日志持久化多层准备：

   持久化类：

   ```java
   @Data
   @Builder
   @AllArgsConstructor
   @NoArgsConstructor
   @TableName("tb_operation_log")
   public class OperationLog {
   
       /**
        * 日志id
        */
       @TableId(value = "id", type = IdType.AUTO)
       private Integer id;
   
       /**
        * 操作模块
        */
       private String optModule;
   
       /**
        * 操作路径
        */
       private String optUrl;
   
       /**
        * 操作类型
        */
       private String optType;
   
       /**
        * 操作方法
        */
       private String optMethod;
   
       /**
        * 操作描述
        */
       private String optDesc;
   
       /**
        * 请求方式
        */
       private String requestMethod;
   
       /**
        * 请求参数
        */
       private String requestParam;
   
       /**
        * 返回数据
        */
       private String responseData;
   
       /**
        * 用户id
        */
       private Integer userId;
   
       /**
        * 用户昵称
        */
       private String nickname;
   
       /**
        * 用户登录ip
        */
       private String ipAddress;
   
       /**
        * ip来源
        */
       private String ipSource;
   
       /**
        * 创建时间
        */
       @TableField(fill = FieldFill.INSERT)
       private LocalDateTime createTime;
   
       /**
        * 修改时间
        */
       @TableField(fill = FieldFill.UPDATE)
       private LocalDateTime updateTime;
   
   }
   ```

   mapper层：

   ```java
   @Repository
   public interface OperationLogMapper extends BaseMapper<OperationLog> {
   }
   ```

3. 准备一个操作日志常量类：

   ```java
   /**
    * @author: ladidol
    * @date: 2022/11/28 14:33
    * @description: 操作日志类型常量
    */
   public class OptTypeConst {
   
       /**
        * 新增操作
        */
       public static final String SAVE_OR_UPDATE = "新增或修改";
   
       /**
        * 新增
        */
       public static final String SAVE = "新增";
   
       /**
        * 修改操作
        */
       public static final String UPDATE = "修改";
   
       /**
        * 删除操作
        */
       public static final String REMOVE = "删除";
   
       /**
        * 上传操作
        */
       public static final String UPLOAD = "上传";
   
   }
   ```

   

4. 将你需要记录操作日志的接口放上这个注解：

   ```java
   /**
        * 保存或更新角色
        *
        * @param roleVO 角色信息
        * @return {@link Result<>}
        */
   @OptLog(optType = SAVE_OR_UPDATE)
   @ApiOperation(value = "保存或更新角色")
   @PostMapping("/admin/role")
   public Result<?> saveOrUpdateRole(@RequestBody @Valid RoleVO roleVO) {
       roleService.saveOrUpdateRole(roleVO);
       return Result.ok();
   }
   
   ```

   

#### 切面处理

1. 新建一个切面

   ```java
   @Aspect
   @Component
   public class OptLogAspect {
   ```

2. 设置操作日志切入点 记录操作日志 在注解的位置切入代码

   ```java
   /**
        * 设置操作日志切入点 记录操作日志 在注解的位置切入代码
        */
   @Pointcut("@annotation(org.cuit.epoch.annotation.OptLog)")
   public void optLogPointCut() {}
   ```

3. 正常返回通知，拦截用户操作日志，连接点正常执行完成后执行， 如果连接点抛出异常，则不会执行

   ```java
   /**
        * 正常返回通知，拦截用户操作日志，连接点正常执行完成后执行， 如果连接点抛出异常，则不会执行
        *
        * @param joinPoint 切入点
        * @param keys      返回结果
        */
   @AfterReturning(value = "optLogPointCut()", returning = "keys")
   @SuppressWarnings("unchecked")
   public void saveOptLog(JoinPoint joinPoint, Object keys) {
       // 获取RequestAttributes
       RequestAttributes requestAttributes = RequestContextHolder.getRequestAttributes();
       // 从获取RequestAttributes中获取HttpServletRequest的信息
       HttpServletRequest request = (HttpServletRequest) Objects.requireNonNull(requestAttributes).resolveReference(RequestAttributes.REFERENCE_REQUEST);
       OperationLog operationLog = new OperationLog();
       // 从切面织入点处通过反射机制获取织入点处的方法
       MethodSignature signature = (MethodSignature) joinPoint.getSignature();
       // 获取切入点所在的方法
       Method method = signature.getMethod();
       // 获取操作
       Api api = (Api) signature.getDeclaringType().getAnnotation(Api.class);
       ApiOperation apiOperation = method.getAnnotation(ApiOperation.class);
       OptLog optLog = method.getAnnotation(OptLog.class);
       // 操作模块
       operationLog.setOptModule(api.tags()[0]);
       // 操作类型
       operationLog.setOptType(optLog.optType());
       // 操作描述
       operationLog.setOptDesc(apiOperation.value());
       // 获取请求的类名
       String className = joinPoint.getTarget().getClass().getName();
       // 获取请求的方法名
       String methodName = method.getName();
       methodName = className + "." + methodName;
       // 请求方式
       operationLog.setRequestMethod(Objects.requireNonNull(request).getMethod());
       // 请求方法
       operationLog.setOptMethod(methodName);
       // 请求参数
       operationLog.setRequestParam(JSON.toJSONString(joinPoint.getArgs()));
       // 返回结果
       operationLog.setResponseData(JSON.toJSONString(keys));
       // 请求用户ID
       operationLog.setUserId(StpUtil.getLoginIdAsInt());
       // 请求用户
       UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
       operationLog.setNickname(userDetailDTO.getNickname());
       // 请求IP
       String ipAddress = IpUtils.getIpAddress(request);
       operationLog.setIpAddress(ipAddress);
       operationLog.setIpSource(IpUtils.getIpSource(ipAddress));
       // 请求URL
       operationLog.setOptUrl(request.getRequestURI());
       operationLogMapper.insert(operationLog);
   }
   ```




### Redis设计

#### 1）在线用户

##### ①数据结构

通过<`String`, `Set<UserDetailDTO>`>的结构，将在线用户存入redis中去

##### ②简介

在用户登录的时候都要在redis中更新一下用户在线情况

```java
//将用户UserInfo存到redis中，方便后序对在线人数进行判断
Set<UserDetailDTO> onlineUsers = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
onlineUsers.add(userDetailDTO);
redisService.set(USER_ONLINE, onlineUsers);
```









#### 用户地域分布情况



#### 说说被点赞情况



#### 文章浏览情况



























## 登录模块

### 1）邮箱密码登录

#### 参数

参数是email+password

#### 简介

用户通过数据库中已经有的邮箱账号+密码来登录。

#### 实现细节

先查询账号是不是存在的（账号合理性）

先得到用户的详细信息UserDetailDTO

判断用户是否存在：

```java
// 查询账号是否存在
UserAuth userAuth = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                                             .select(UserAuth::getId, UserAuth::getUserInfoId, UserAuth::getUsername, UserAuth::getPassword, UserAuth::getLoginType)
                                             .eq(UserAuth::getUsername, username));
if (Objects.isNull(userAuth)) {
    throw new AppException("用户名不存在!");
}
```



判断密码正确性：

```java
if (!userDetailDTO.getPassword().equals(PasswordUtils.encrypt(password))) {
    throw new AppException("密码错误！");
}
```

判断是否被禁用：

```java
// 判断账号是否禁用
if (userDetailDTO.getIsDisable().equals(TRUE)) {
    throw new AppException("账号已被禁用");
}
```



这里通过Satoken登录就直接调用`StpUtil.login()`登录就行，同时将用户详细信息存入session，这里也可以考略将他们存入Redis中去。

```java
//sa-token登录
StpUtil.login(userDetailDTO.getId());
//将用户角色信息存入session中
StpUtil.getSession().set(USER_ROLE,userDetailDTO.getRoleList());
//将用户详细信息存入session中
StpUtil.getSession().set(USER_INFO,userDetailDTO);
```

将用户登录情况更新到redis中去

```java
//将用户UserInfo存到redis中，方便后序对在线人数进行判断
Set<UserDetailDTO> onlineUsers = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
onlineUsers.add(userDetailDTO);
redisService.set(USER_ONLINE, onlineUsers);
```

其中用户详细信息`userDetailDTO`通过`convertUserDetail(userAuth, request)`得到：

```java
// 查询账号信息
UserInfo userInfo = userInfoMapper.selectById(user.getUserInfoId());
// 查询账号角色
List<String> roleList = roleMapper.listRolesByUserInfoId(userInfo.getId());
// 查询账号点赞信息
Set<Object> articleLikeSet = redisService.sMembers(ARTICLE_USER_LIKE + userInfo.getId());
Set<Object> commentLikeSet = redisService.sMembers(COMMENT_USER_LIKE + userInfo.getId());
Set<Object> talkLikeSet = redisService.sMembers(TALK_USER_LIKE + userInfo.getId());
// 获取设备信息
String ipAddress = IpUtils.getIpAddress(request);
String ipSource = IpUtils.getIpSource(ipAddress);
UserAgent userAgent = IpUtils.getUserAgent(request);
```

同时每一次登录后都需要在数据库中更新用户的一些登录信息

```java
// 更新用户ip，最近登录时间
updateUserInfo(userDetailDTO);
```

登录成功后返回给前端用户详细信息：

```json
{
  "flag": true,
  "code": 20000,
  "message": "操作成功",
  "data": {
    "id": 998,
    "userInfoId": 1008,
    "email": "test2@qq.com",
    "loginType": 1,
    "username": "test2@qq.com",
    "password": "c74792028820c759ec7165da026403526fbb17163c96b485fc38d728ceaa2648",
    "roleList": [
      "user"
    ],
    "nickname": "test2",
    "avatar": "http://dev-myblog.oss-cn-hangzhou.aliyuncs.com/avatar/39fb0f58713b554cb18e8b801c3119bb.jpg",
    "intro": "这里是test2，我们直接用session来说话了",
    "webSite": "https://blog.ladidol.top",
    "articleLikeSet": [],
    "commentLikeSet": [],
    "talkLikeSet": [],
    "ipAddress": "0:0:0:0:0:0:0:1",
    "ipSource": "",
    "isDisable": 0,
    "browser": "Chrome 10",
    "os": "Windows 10",
    "lastLoginTime": "2022-11-24T15:17:46.845"
  }
}
```

### 2）注销登录

#### 参数

前端自动带cookie访问

#### 简介

直接将当前用户从Satoken中logout就行了

#### 实现细节

1. 先从redis在线目录中删除

   ```java
   //将用户UserInfo从redis中删除
   Set<Integer> onlineUsers = (Set<Integer>) redisService.get(USER_ONLINE);
   UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
   onlineUsers.remove(userDetailDTO);
   redisService.set(USER_ONLINE, onlineUsers);
   ```

2. 再从satoken中退出登录

   ```java
   StpUtil.logout();
   ```

   



### 3）微博第三方登录

#### 参数

通过微博登录得到的openId

#### 简介

携带微博登录的到的openId访问该接口

#### 实现细节

对微博登录的实现可以先看一下这篇文章[springboot实现微博登录 - 腾讯云开发者社区-腾讯云 (tencent.com)](https://cloud.tencent.com/developer/article/1640072)

1. 最先访问接口：从而可以openId；`https://api.weibo.com/oauth2/authorize?client_id=你的appkey&response_type=code&redirect_uri=你的回调地址`

2. 前端对博客后端发起请求：携带参数`openId`访问你的回调地址`http://blog.ladidol.top/oauth/login/weibo`

3. 后端访问这个接口https://api.weibo.com/oauth2/access_token，并使用openid来的到用户的access_token和用户的uid

   ```java
   // 根据code换取微博uid和accessToken
   MultiValueMap<String, String> weiboData = new LinkedMultiValueMap<>();
   // 定义微博token请求参数
   weiboData.add(CLIENT_ID, weiboConfigProperties.getAppId());
   weiboData.add(CLIENT_SECRET, weiboConfigProperties.getAppSecret());
   weiboData.add(GRANT_TYPE, weiboConfigProperties.getGrantType());
   weiboData.add(REDIRECT_URI, weiboConfigProperties.getRedirectUrl());
   weiboData.add(CODE, weiBoLoginVO.getCode());//前端传过来的回调code（openId）
   HttpEntity<MultiValueMap<String, String>> requestEntity = new HttpEntity<>(weiboData, null);
   try {
       //通过post访问https://api.weibo.com/oauth2/access_token带上这五个请求参数
       return restTemplate.exchange(weiboConfigProperties.getAccessTokenUrl(), HttpMethod.POST, requestEntity, WeiboTokenDTO.class).getBody();
   } catch (Exception e) {
       throw new AppException(WEIBO_LOGIN_ERROR);
   }
   ```

4. 因为本博客要登录返回给前端用户的基本信息，这里需要判断用户是否已经通过微博第三方登录过了：

   ```java
   // 判断是否已注册
   UserAuth user = getUserAuth(socialToken);
   if (Objects.nonNull(user)) {
       // 返回数据库用户信息
       userDetailDTO = getUserDetail(user, ipAddress, ipSource);
   } else {
       // 获取第三方用户信息，保存到数据库返回
       userDetailDTO = saveUserDetail(socialToken, ipAddress, ipSource);
   }
   ```

5. 如果未登录需要通过请求`https://api.weibo.com/2/users/show.json?uid={uid}&access_token={access_token}`

   ```java
   // 定义请求参数
   Map<String, String> data = new HashMap<>(2);
   data.put(UID, socialTokenDTO.getOpenId());
   data.put(ACCESS_TOKEN, socialTokenDTO.getAccessToken());
   // 获取微博用户信息
   WeiboUserInfoDTO weiboUserInfoDTO = restTemplate.getForObject(weiboConfigProperties.getUserInfoUrl(), WeiboUserInfoDTO.class, data);
   // 返回用户信息
   return SocialUserInfoDTO.builder()
       .nickname(Objects.requireNonNull(weiboUserInfoDTO).getScreen_name())
       .avatar(weiboUserInfoDTO.getAvatar_hd())
       .build();
   ```

   得到用户的头像avatar和昵称nickname最终和注册一样的逻辑，给该新用户填写基本信息

   ```java
   /**
        * 新增用户信息
        *
        * @param socialToken token信息
        * @param ipAddress   ip地址
        * @param ipSource    ip源
        * @return {@link UserDetailDTO} 用户信息
        */
   private UserDetailDTO saveUserDetail(SocialTokenDTO socialToken, String ipAddress, String ipSource) {
       // 获取第三方用户信息
       SocialUserInfoDTO socialUserInfo = getSocialUserInfo(socialToken);
       // 保存用户信息
       UserInfo userInfo = UserInfo.builder()
           .nickname(socialUserInfo.getNickname())
           .avatar(socialUserInfo.getAvatar())
           .build();
       userInfoDao.insert(userInfo);
       // 保存账号信息
       UserAuth userAuth = UserAuth.builder()
           .userInfoId(userInfo.getId())
           .username(socialToken.getOpenId())
           .password(socialToken.getAccessToken())
           .loginType(socialToken.getLoginType())
           .lastLoginTime(LocalDateTime.now(ZoneId.of(SHANGHAI.getZone())))
           .ipAddress(ipAddress)
           .ipSource(ipSource)
           .build();
       userAuthDao.insert(userAuth);
       // 绑定角色
       UserRole userRole = UserRole.builder()
           .userId(userInfo.getId())
           .roleId(RoleEnum.USER.getRoleId())
           .build();
       userRoleDao.insert(userRole);
       return userAuthService.convertUserDetail(userAuth, request);
   }
   ```

   

#### 提醒

1. 值得注意的是第三方登录得到的username是openId，只是可以在userinfo表里面设置邮箱地址方便通知！
2. 这里面后端通过`RestTemplate`来带参数发请求
3. 这里用了一下策略模式来管理规范遵循`oauth2`第三方登录
3. [策略模式初见 (talkxj.com)](https://www.talkxj.com/articles/30)这里有对策略模式的一些使用，感觉好帅。

### 4）QQ第三方登录

#### 参数

通过QQ登录得到的openId

todo，待做

#### 简介

携带QQ登录的到的openId访问该接口

#### 实现细节

[项目配置介绍 (talkxj.com)](https://www.talkxj.com/articles/3)

[理解OAuth 2.0 - 阮一峰的网络日志 (ruanyifeng.com)](http://www.ruanyifeng.com/blog/2014/05/oauth_2_0.html)

![web实现QQ第三方登录_编程](https://figurebed-ladidol.oss-cn-chengdu.aliyuncs.com/img/202211241958214.webp)





todo













## 用户账号模块

### 1）用户注册

#### 参数

```java
{
  "code": {},
  "password": {},
  "username": {}
}
```

#### 简介

通过携带邮箱验证码+你的邮箱账号+你的密码进行注册。

#### 实现细节

1. 检测邮箱和邮箱验证码是否合法

   ```java
   /**
        * 校验用户数据是否合法
        *
        * @param user 用户数据
        * @return 结果
        */
   private Boolean checkUser(UserVO user) {
       if (!user.getCode().equals(redisService.get(USER_CODE_KEY + user.getUsername()))) {
           throw new AppException("验证码错误！");
       }
       //查询用户名是否存在
       UserAuth userAuth = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                                                    .select(UserAuth::getUsername)
                                                    .eq(UserAuth::getUsername, user.getUsername()));
       return Objects.nonNull(userAuth);
   }
   ```

   

2. 封装用户信息对象、用户角色对象、用户账号对象分别插入数据

	```java
	// 校验账号是否合法
	if (checkUser(user)) {
    throw new AppException("邮箱已被注册！");
	}
	// 新增用户信息
	UserInfo userInfo = UserInfo.builder()
    .email(user.getUsername())
    .nickname(CommonConst.DEFAULT_NICKNAME + IdWorker.getId())
    .avatar(blogInfoService.getWebsiteConfig().getUserAvatar())
    .build();
	userInfoMapper.insert(userInfo);
	// 绑定用户角色
	UserRole userRole = UserRole.builder()
    .userId(userInfo.getId())
    .roleId(RoleEnum.USER.getRoleId())
    .build();
	userRoleMapper.insert(userRole);
	// 新增用户账号
	UserAuth userAuth = UserAuth.builder()
    .userInfoId(userInfo.getId())
    .username(user.getUsername())
    .password(PasswordUtils.encrypt(user.getPassword()))
    .loginType(LoginTypeEnum.EMAIL.getType())
    .build();
	userAuthMapper.insert(userAuth);
	```
	
3. 更新一下用户的地区分布情况，该方法也用@Scheduled注解标记为定时方法，详细实现在本模块的第三个接口中会详细介绍

  ```java
  //更新在redis中用户地区信息
  statisticalUserArea();
  ```

  

### 2）发送邮箱验证码

#### 参数

username即邮箱账号

#### 简介

通过向邮箱账号发送验证码请求，用户会在该邮箱中收到验证码。该验证码用于注册账号用。

#### 实现细节

1. 通过随机函数生成指定位数的随机二维码，并构造邮件对象

   ```java
   // 校验账号是否合法
   if (!checkEmail(username)) {
       throw new AppException("请输入正确邮箱");
   }
   // 生成六位随机验证码发送
   String code = getRandomCode();
   // 发送验证码
   EmailDTO emailDTO = EmailDTO.builder()
       .email(username)
       .subject("ladidol'blog 的验证码")
       .content("您的验证码为 " + code + " 有效期15分钟，请不要告诉他人哦！")
       .build();
   ```

2. 通过消息队列RabbitMQ来进行发送，减少发邮件的时间等待，同时没有另起一个线程的资源损耗。

   ```java
   rabbitTemplate.convertAndSend(EMAIL_EXCHANGE, "*", new Message(JSON.toJSONBytes(emailDTO), new MessageProperties()));
   ```

3. 将验证码存入redis中方便注册的时候进行验证码验证

   ```java
   // 将验证码存入redis，设置过期时间为15分钟
   redisService.set(USER_CODE_KEY + username, code, CODE_EXPIRE_TIME);
   ```




### 3）获取用户区域分布

#### 参数

封装在conditionVO中的type参数

#### 简介

type参数：1表示查询用户的地区分布，2表示查询游客的地区分布

其中游客数据主要来自于游客查看博客等会触发redis更新。

#### 实现细节

1. 用`List<UserAreaDTO>`来返回给前端

2. 用枚举类+switchcase的方式来实现对用户和游客的分别查询：

   ```java
   switch (Objects.requireNonNull(getUserAreaType(conditionVO.getType()))) {
           //默认查询用户的
       case USER_VISITOR:
           // 查询注册用户区域分布
           
   		......
               
           return userAreaDTOList;
       case VISITOR:
           // 查询游客区域分布
           
           ......
               
           return userAreaDTOList;
       default:
           break;
   }
   ```

3. 查询用户：

   ```java
   //默认查询用户的
   case USER_VISITOR:
       // 查询注册用户区域分布
       Object userArea = redisService.get(USER_AREA);
       if (Objects.nonNull(userArea)) {
           userAreaDTOList = JSON.parseObject(userArea.toString(), List.class);
       }
       return userAreaDTOList;
   ```

4. 查询游客：

   ```java
   case VISITOR:
       // 查询游客区域分布
       Map<String, Object> visitorArea = redisService.hGetAll(VISITOR_AREA);
       if (Objects.nonNull(visitorArea)) {
           userAreaDTOList = visitorArea.entrySet().stream()
               .map(item -> UserAreaDTO.builder()
                    .name(item.getKey())
                    .value(Long.valueOf(item.getValue().toString()))
                    .build())
               .collect(Collectors.toList());
       }
       return userAreaDTOList;
   ```



### 4）更新redis中用户区域分布情况

#### 参数

无

#### 简介

这只是一个服务，没有提供接口。主要是更新redis中用户区域的分布情况用`RedisPrefixConst.USER_AREA`作为key，将地区和地区出现数量放入value中去。

#### 实现细节

1. 该类使用了定时任务：方便定时更新信息

   ```java
   /**
        * 统计用户地区
        */
   @Scheduled(cron = "0 0 * * * ?") // [秒] [分] [小时] [日] [月] [周] [年]，问号表示不关心星期几，每天每小时整就会触发一下这个方法
   public void statisticalUserArea() {
   ```

2. 先查询用户的账号信息：

   ```java
   List<UserAuth> userAuths = userAuthMapper.selectList(new LambdaQueryWrapper<UserAuth>().select(UserAuth::getIpSource,UserAuth::getUsername));
   
   ```

3. 依旧用Stream对userAuths遍历，在计数的同时还处理了一下省份信息：（很帅）

   ```java
   Map<String, Long> userAreaMap = userAuths
       .stream()
       .map(item -> {
           if (StringUtils.isNotBlank(item.getIpSource())) {
               return item.getIpSource().substring(0, 2)
                   .replaceAll(PROVINCE, "")
                   .replaceAll(CITY, "");
           }
           return UNKNOWN;
       })
       .collect(Collectors.groupingBy(item -> item, Collectors.counting()));
   ```

4. 将map的键值形式转化成UserAreaDTO属性：

   ```java
   // 转换格式
   List<UserAreaDTO> userAreaList = userAreaMap.entrySet().stream()
       .map(item -> UserAreaDTO.builder()
            .name(item.getKey())
            .count(item.getValue())
            .build())
       .collect(Collectors.toList());
   ```

5. 最后将用户地区统计放到redis中

   ```java
   redisService.set(USER_AREA, JSON.toJSONString(userAreaList));
   ```

### 5）查询后台用户列表

#### 参数

页码和页面大小

#### 简介

通过分页查询后台用户列表。

#### 实现细节

1. 先获取用户总数量

   ```java
   // 获取后台用户数量
   Integer count = userAuthMapper.countUser(condition);
   if (count == 0) {
       return new PageResult<>();
   }
   ```

   mapper层

   ```java
   /**
        * 查询后台用户数量
        *
        * @param condition 条件
        * @return 用户数量
        */
   Integer countUser(@Param("condition") ConditionVO condition);
   ```

   ```xml
   <select id="countUser" resultType="java.lang.Integer">
       SELECT
       count( 1 )
       FROM
       tb_user_auth ua
       LEFT JOIN tb_user_info ui ON ua.user_info_id = ui.id
       <where>
           <if test="condition.keywords != null">
               nickname like concat('%',#{condition.keywords},'%')
           </if>
           <if test="condition.loginType != null">
               and login_type = #{condition.loginType}
           </if>
       </where>
   </select>
   ```

2. 再获取全部后台用户列表

   ```java
   // 获取后台用户列表
   List<UserBackDTO> userBackDTOList = userAuthMapper.listUsers(PageUtils.getLimitCurrent(), PageUtils.getSize(), condition);
   return new PageResult<>(userBackDTOList, count);
   ```

   mapper层：

   ```java
   /**
        * 查询后台用户列表
        *
        * @param current   页码
        * @param size      大小
        * @param condition 条件
        * @return {@link List <UserBackDTO>} 用户列表
        */
   List<UserBackDTO> listUsers(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);
   
   ```

   ```xml
   <resultMap id="UserBackMap" type="org.cuit.epoch.dto.UserBackDTO">
       <id column="id" property="id"/>
       <result column="user_info_id" property="userInfoId"/>
       <result column="avatar" property="avatar"/>
       <result column="nickname" property="nickname"/>
       <result column="login_type" property="loginType"/>
       <result column="ip_address" property="ipAddress"/>
       <result column="ip_source" property="ipSource"/>
       <result column="create_time" property="createTime"/>
       <result column="last_login_time" property="lastLoginTime"/>
       <result column="is_disable" property="isDisable"/>
       <collection property="roleList" ofType="org.cuit.epoch.dto.UserRoleDTO">
           <id column="role_id" property="id"/>
           <id column="role_name" property="roleName"/>
       </collection>
   </resultMap>
   
   <select id="listUsers" resultMap="UserBackMap">
       SELECT
       ua.id,
       user_info_id,
       avatar,
       nickname,
       login_type,
       r.id as role_id,
       role_name,
       ip_address,
       ip_source,
       ua.create_time,
       last_login_time,
       ui.is_disable
       FROM
       (
       SELECT
       id,
       avatar,
       nickname,
       is_disable
       FROM
       tb_user_info
       <where>
           <if test="condition.loginType != null">
               id in
               (
               SELECT
               user_info_id
               FROM
               tb_user_auth
               WHERE
               login_type =  #{condition.loginType}
               )
           </if>
           <if test="condition.keywords != null">
               and nickname like concat('%',#{condition.keywords},'%')
           </if>
       </where>
       LIMIT #{current},#{size}
       )   ui
       LEFT JOIN tb_user_auth ua ON ua.user_info_id = ui.id
       LEFT JOIN tb_user_role ur ON ui.id = ur.user_id
       LEFT JOIN tb_role r ON ur.role_id = r.id
   </select>
   ```

### 6）用户修改密码

#### 参数

```java
{
  "code": {邮箱验证码},
  "password": {新密码},
  "username": {邮箱账号}
}
```

#### 简介

主要是用于用户忘记密码操作，通过重新邮箱验证，再登录

#### 实现细节

1. 先验证码账号的合法性

   ```java
   // 校验账号是否合法，同时有邮箱验证码验证
   if (!checkUser(user)) {
       throw new AppException("邮箱尚未注册！");
   }
   ```

2. 根据用户名修改密码

   ```java
   // 根据用户名修改密码
   userAuthMapper.update(new UserAuth(), new LambdaUpdateWrapper<UserAuth>()
                         .set(UserAuth::getPassword, PasswordUtils.encrypt(user.getPassword()))
                         .eq(UserAuth::getUsername, user.getUsername()));
   ```

### 7）管理员修改密码

#### 参数

```java
{
  "newPassword": {新密码},
  "oldPassword": {老密码}
}
```

#### 简介

用于管理员后台自己修改密码，但是需要之前旧密码才行

#### 实现细节

1. 查询旧密码

   ```java
   // 查询旧密码是否正确
   UserAuth user = userAuthMapper.selectOne(new LambdaQueryWrapper<UserAuth>()
                                            .eq(UserAuth::getId, StpUtil.getLoginIdAsInt()));
   ```

2. 正确则修改密码，错误则提示不正确

   ```java
   // 正确则修改密码，错误则提示不正确
   if (Objects.nonNull(user) && PasswordUtils.match(passwordVO.getOldPassword(), user.getPassword())) {
       UserAuth userAuth = UserAuth.builder()
           .id(StpUtil.getLoginIdAsInt())
           .password(PasswordUtils.encrypt(passwordVO.getNewPassword()))
           .build();
       userAuthMapper.updateById(userAuth);
   } else {
       throw new AppException("旧密码不正确");
   }
   ```

   





## 用户信息模块

### 1）更新用户头像

#### 参数

参数就是头像的图片文件

#### 简介

前端将选好的图片传给后端

#### 实现细节

1. 头像文件的上传

   ```java
   // 头像上传
   String avatar = uploadStrategyContext.executeUploadStrategy(file, FilePathEnum.AVATAR.getPath());
   ```

2. 然后更新用户信息表userInfo中的avatar字段。

3. @Transactional(rollbackFor = Exception.class)

#### 提醒

这里文件上传，也用了策略模式，这样更好管理多种上传策略，比如：本地上传、阿里云oss、腾讯云cos；





### 2）绑定用户邮箱

#### 参数

```json
{
  "code": {邮箱验证码},
  "email": {邮箱}
}
```

#### 简介

前端发出发送邮箱验证码的请求，然后再通过邮箱验证码+邮箱来更换登录用户userInfo中的绑定的邮箱，期间都是要通过携带cookie访问的

#### 实现细节

1. 验证码验证+userInfo表中email字段的更改。
2. @Transactional(rollbackFor = Exception.class)



### 3）更新用户信息

#### 参数

```json
{
  "intro": {个人简介},
  "nickname": {昵称},
  "webSite": {个人网站}
}
```

#### 简介

普通用户修改个人信息的接口，期间都是要通过携带cookie访问的

#### 实现细节

1. 直接根据当前登录这用户id来修改userInfo中的这三样基本信息

2. ```java
   @Transactional(rollbackFor = Exception.class)
   ```



### 4）后台修改用户角色和昵称

#### 参数

```json
{
  "nickname": {昵称},
  "roleIdList": {所属的全部角色id列表},
  "userInfoId": {用户信息id}
}
```

#### 简介

就是更新用户角色或者昵称的一个接口

#### 实现细节

1. 先根据id更新用户昵称

   ```java
   // 更新用户角色和昵称
   UserInfo userInfo = UserInfo.builder()
       .id(userRoleVO.getUserInfoId())
       .nickname(userRoleVO.getNickname())
       .build();
   userInfoDao.updateById(userInfo);
   ```

2. 更新用户角色，就是先删除原来的用户角色，再根据roleidlist添加进UserRole表中

   ```java
   // 删除用户角色重新添加
   userRoleService.remove(new LambdaQueryWrapper<UserRole>()
                          .eq(UserRole::getUserId, userRoleVO.getUserInfoId()));
   List<UserRole> userRoleList = userRoleVO.getRoleIdList().stream()
       .map(roleId -> UserRole.builder()
            .roleId(roleId)
            .userId(userRoleVO.getUserInfoId())
            .build())
       .collect(Collectors.toList());
   userRoleService.saveBatch(userRoleList);
   ```

3. 加个事务`@Transactional(rollbackFor = Exception.class)`

### 5）后台修改用户账号的封禁情况

#### 参数

```json
{
  "id": 0,
  "isDisable": 0
}
```

#### 简介

更新用户封禁情况，默认0不封禁，1封禁

#### 实现细节

1. 根据用户信息id来更新用户封禁情况

   ```java
   // 更新用户禁用状态
   UserInfo userInfo = UserInfo.builder()
       .id(userDisableVO.getId())
       .isDisable(userDisableVO.getIsDisable())
       .build();
   userInfoDao.updateById(userInfo);
   ```

2. 登录的时候会判断用户账号的可用情况。

### 6）查看在线用户

#### 参数

size，current，keywords

#### 简介

可以模糊查询、分页查询的接口。

#### 实现细节

1. 从redis中获取全部在线用户

   ```java
   // 从redis中获取全部在线用户
   Set<UserDetailDTO> onlineUser = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
   ```

2. 将在线用户封装在UserOnlineDTO中

   ```java
   List<UserOnlineDTO> userOnlineDTOList = onlineUser.stream()
       .filter(item -> onlineUser.size() > 0)
       .map(item -> JSON.parseObject(JSON.toJSONString(item), UserOnlineDTO.class))
       .filter(item -> StringUtils.isBlank(conditionVO.getKeywords()) || item.getNickname().contains(conditionVO.getKeywords()))
       .sorted(Comparator.comparing(UserOnlineDTO::getLastLoginTime).reversed())
       .collect(Collectors.toList());
   ```

3. 执行分页操作

   ```java
   // 执行分页
   int fromIndex = PageUtils.getLimitCurrent().intValue();
   int size = PageUtils.getSize().intValue();
   int toIndex = userOnlineDTOList.size() - fromIndex > size ? fromIndex + size : userOnlineDTOList.size();
   List<UserOnlineDTO> userOnlineList = userOnlineDTOList.subList(fromIndex, toIndex);
   return new PageResult<>(userOnlineList, userOnlineDTOList.size());
   ```

   

### 7）下线指定用户

#### 参数

userInfoId

#### 简介

通过userInfoId下线

#### 实现细节

1. 从redis中获取全部在线用户

   ```java
   // 从redis中获取全部在线用户
   Set<UserDetailDTO> onlineUsers = (Set<UserDetailDTO>) redisService.get(USER_ONLINE);
   ```

2. 得到需要下线的用户信息

   ```
   // 得到指定的
   List<UserDetailDTO> userInfoList = onlineUsers.stream().filter(item -> {
       UserDetailDTO userDetailDTO = (UserDetailDTO) item;
       return userDetailDTO.getUserInfoId().equals(userInfoId);
   }).collect(Collectors.toList());
   ```

3. 将其从redis中和它的session去除

   ```java
   for (UserDetailDTO userDetailDTO : userInfoList) {
       log.info("踢下线： " + userDetailDTO);
       StpUtil.logout(userDetailDTO.getId());
       onlineUsers.remove(userDetailDTO);
   }
   ```

4. 重新维护redis中的全部在线人数

   ```java
   redisService.set(USER_ONLINE, onlineUsers);
   ```

   











## 博客信息模块

### 1）查看博客主页基本信息

#### 参数

无参数。

#### 简介

就是博客首页，但是这个博客首页后端直接返回给前端一些基本信息，比如博客网站的基本信息、是否展示某些页面、是否可用一些功能、博客全部页面的基本信息（首页，归档，分类，标签等等）。从而可以提高一些页面的访问速度。

#### 实现细节

1. 依旧是分开查询不同的表不同的信息，然后封装到同一个对象中

   ```java
   // 查询文章数量
   Integer articleCount = articleDao.selectCount(new LambdaQueryWrapper<Article>()
                                                 .eq(Article::getStatus, PUBLIC.getStatus())
                                                 .eq(Article::getIsDelete, FALSE));
   // 查询分类数量
   Integer categoryCount = categoryDao.selectCount(null);
   // 查询标签数量
   Integer tagCount = tagDao.selectCount(null);
   // 查询访问量
   Object count = redisService.get(BLOG_VIEWS_COUNT);
   String viewsCount = Optional.ofNullable(count).orElse(0).toString();
   // 查询网站配置
   WebsiteConfigVO websiteConfig = this.getWebsiteConfig();
   // 查询页面图片
   List<PageVO> pageVOList = pageService.listPages();
   // 封装数据
   return BlogHomeInfoDTO.builder()
       .articleCount(articleCount)
       .categoryCount(categoryCount)
       .tagCount(tagCount)
       .viewsCount(viewsCount)
       .websiteConfig(websiteConfig)
       .pageList(pageVOList)
       .build();
   ```

2. 访问量直接放到redis中去了，直接访问redis中，如果为零有一个很流啤的处理方法`String viewsCount = Optional.ofNullable(count).orElse(0).toString();`

3. 查询网站配置也是先通过redis访问，没有就从数据库中访问

   ```java
   // 获取缓存数据
   Object websiteConfig = redisService.get(WEBSITE_CONFIG);
   if (Objects.nonNull(websiteConfig)) {
       websiteConfigVO = JSON.parseObject(websiteConfig.toString(), WebsiteConfigVO.class);
   } else {
       // 从数据库中加载
       String config = websiteConfigDao.selectById(DEFAULT_CONFIG_ID).getConfig();
       websiteConfigVO = JSON.parseObject(config, WebsiteConfigVO.class);
       redisService.set(WEBSITE_CONFIG, config);
   }
   return websiteConfigVO;
   ```

4. 页面信息，调用了**页面模块**中的listPages方法，和网站配置一样redis-mysql持久化访问机制。

## 后台菜单模块

### 1）查看当前用户菜单列表

#### 参数

需要带cookie访问的同时要从里面的到userInfoId方便后面操作

#### 简介

通过当前登录者的userInfoId得到用户能访问的菜单组件列表。

#### 实现细节

1. 的到用户的能访问的菜单列表`menuList`：

   ```java
   // 查询用户菜单信息（根据用户查询捏）
   UserDetailDTO userDetailDTO = (UserDetailDTO) StpUtil.getSession().get(USER_INFO);
   List<Menu> menuList = menuMapper.listMenusByUserInfoId(userDetailDTO.getUserInfoId());
   ```

2. 获取主目录列表`catalogList`:

   ```java
   // 获取主目录列表
   List<Menu> catalogList = listCatalog(menuList);
   ```

   `listCatalog`通过filter筛选parentId字段**为空**的目录，并将他们重新用list返回

   ```java
   /**
        * 获取目录列表
        *
        * @param menuList 菜单列表
        * @return 目录列表
        */
   private List<Menu> listCatalog(List<Menu> menuList) {
       return menuList.stream()
           .filter(item -> Objects.isNull(item.getParentId()))
           .sorted(Comparator.comparing(Menu::getOrderNum))
           .collect(Collectors.toList());
   }
   ```

3. 获取每一个主目录下的子菜单`childrenMap`:

   ```java
   // 获取主目录下的子菜单
   Map<Integer, List<Menu>> childrenMap = getMenuMap(menuList);
   ```

   `getMenuMap`通过filter筛选parentId字段是**不为空**的目录，并将他们用map返回，key为parentId，value为对应下的子菜单list

   ```java
   /**
        * 获取目录下菜单列表
        *
        * @param menuList 菜单列表
        * @return 目录下的菜单列表
        */
   private Map<Integer, List<Menu>> getMenuMap(List<Menu> menuList) {
       return menuList.stream()
           .filter(item -> Objects.nonNull(item.getParentId()))
           .collect(Collectors.groupingBy(Menu::getParentId));
   }
   ```

4. 转换目录结构，发给前端就成了主目录+其子目录的格式

   ```java
   /**
        * 转换用户菜单格式
        *
        * @param catalogList 目录
        * @param childrenMap 子菜单
        */
   private List<UserMenuDTO> convertUserMenuList(List<Menu> catalogList, Map<Integer, List<Menu>> childrenMap) {
       return catalogList.stream().map(item -> {
           // 获取目录
           UserMenuDTO userMenuDTO = new UserMenuDTO();
           List<UserMenuDTO> list = new ArrayList<>();
           // 获取目录下的子菜单
           List<Menu> children = childrenMap.get(item.getId());
           if (CollectionUtils.isNotEmpty(children)) {
               // 多级菜单处理
               userMenuDTO = BeanCopyUtils.copyObject(item, UserMenuDTO.class);
               list = children.stream()
                   .sorted(Comparator.comparing(Menu::getOrderNum))
                   .map(menu -> {
                       UserMenuDTO dto = BeanCopyUtils.copyObject(menu, UserMenuDTO.class);
                       dto.setHidden(menu.getIsHidden().equals(TRUE));
                       return dto;
                   })
                   .collect(Collectors.toList());
           } else {
               // 一级菜单处理
               userMenuDTO.setPath(item.getPath());
               userMenuDTO.setComponent(COMPONENT);
               list.add(UserMenuDTO.builder()
                        .path("")
                        .name(item.getName())
                        .icon(item.getIcon())
                        .component(item.getComponent())
                        .build());
           }
           userMenuDTO.setHidden(item.getIsHidden().equals(TRUE));
           userMenuDTO.setChildren(list);
           return userMenuDTO;
       }).collect(Collectors.toList());
   }
   ```



### 2）查看全部菜单列表

#### 参数

关键字keywords，用类ConditionVO来获取

#### 简介

类似前面单个用户的菜单列表获取，只是这里根据关键字keywords来获取列表

#### 实现细节

1. 也是先得到主目录和对应子目录，然后进行拼接操作。

   ```java
   @Override
   public List<MenuDTO> listMenus(ConditionVO conditionVO) {
       // 查询菜单数据(获取全部菜单)
       List<Menu> menuList = menuMapper.selectList(new LambdaQueryWrapper<Menu>()
                                                   .like(StringUtils.isNotBlank(conditionVO.getKeywords()), Menu::getName, conditionVO.getKeywords()));
       log.info("获取目录列表 menuList = " + menuList);
       // 获取目录列表
       List<Menu> catalogList = listCatalog(menuList);
       log.info("获取目录列表 catalogList = " + catalogList);
       // 获取目录下的子菜单
       Map<Integer, List<Menu>> childrenMap = getMenuMap(menuList);
       // 组装目录菜单数据
       List<MenuDTO> menuDTOList = catalogList.stream().map(item -> {
           MenuDTO menuDTO = BeanCopyUtils.copyObject(item, MenuDTO.class);
           // 获取目录下的菜单排序
           List<MenuDTO> list = BeanCopyUtils.copyList(childrenMap.get(item.getId()), MenuDTO.class).stream()
               .sorted(Comparator.comparing(MenuDTO::getOrderNum))
               .collect(Collectors.toList());
           menuDTO.setChildren(list);
           childrenMap.remove(item.getId());
           return menuDTO;
       }).sorted(Comparator.comparing(MenuDTO::getOrderNum)).collect(Collectors.toList());
       // 若还有菜单未取出则拼接
       if (CollectionUtils.isNotEmpty(childrenMap)) {
           List<Menu> childrenList = new ArrayList<>();
           childrenMap.values().forEach(childrenList::addAll);
           List<MenuDTO> childrenDTOList = childrenList.stream()
               .map(item -> BeanCopyUtils.copyObject(item, MenuDTO.class))
               .sorted(Comparator.comparing(MenuDTO::getOrderNum))
               .collect(Collectors.toList());
           menuDTOList.addAll(childrenDTOList);
       }
       return menuDTOList;
   }
   ```

   

### 3）新增或修改菜单

#### 参数

```json
{
  "component": {},
  "icon": {},
  "id": {},
  "isHidden": {},
  "name": {},
  "orderNum": {},
  "parentId": {},
  "path": {}
}
```

#### 简介

新增或修改菜单，

#### 实现细节

1. 这个copyObject的方式来直接快速调用Mybatis-Plus的方法，直接一个赞！

   ```java
   Menu menu = BeanCopyUtils.copyObject(menuVO, Menu.class);
   this.saveOrUpdate(menu);
   ```

### 4）删除菜单

#### 参数

menuId

#### 简介

通过menuId来删除菜单，但是删除的菜单必须满足不和其他用户有绑定的

#### 实现细节

1. 先通过**角色—菜单**表查询是否有角色和该菜单关联：

   ```java
   // 查询是否有角色关联
   Integer count = roleMenuDao.selectCount(new LambdaQueryWrapper<RoleMenu>()
                                           .eq(RoleMenu::getMenuId, menuId));
   if (count > 0) {
       throw new AppException("菜单下有角色关联");
   }
   ```

2. 查询改菜单的子菜单，得到ids链表，一起删除全部：

   ```java
   // 查询子菜单
   List<Integer> menuIdList = menuMapper.selectList(new LambdaQueryWrapper<Menu>()
                                                    .select(Menu::getId)
                                                    .eq(Menu::getParentId, menuId))
       .stream()
       .map(Menu::getId)
       .collect(Collectors.toList());
   menuIdList.add(menuId);
   menuMapper.deleteBatchIds(menuIdList);
   ```

   

### 5）查看全部角色菜单选项

#### 参数

无

#### 简介

查询全部角色所管理的菜单，这里直接返回全部就行，其实和上面第二个接口有点类似，只是这里可以快速查询。

#### 实现细节

1. 依旧是先得到主目录和对应子目录，然后进行拼接操作。这里用mapper层的select方法方便快捷的查询这四个字段的数据：

   ```java
   // 查询菜单数据(只查询id、name、parentId、orderNum四个字段)
   List<Menu> menuList = menuMapper.selectList(new LambdaQueryWrapper<Menu>()
           .select(Menu::getId, Menu::getName, Menu::getParentId, Menu::getOrderNum));
   // 获取目录列表
   List<Menu> catalogList = listCatalog(menuList);
   // 获取目录下的子菜单
   Map<Integer, List<Menu>> childrenMap = getMenuMap(menuList);
   // 组装目录菜单数据
   return catalogList.stream().map(item -> {
       // 获取目录下的菜单排序
       List<LabelOptionDTO> list = new ArrayList<>();
       List<Menu> children = childrenMap.get(item.getId());
       if (CollectionUtils.isNotEmpty(children)) {
           list = children.stream()
                   .sorted(Comparator.comparing(Menu::getOrderNum))
                   .map(menu -> LabelOptionDTO.builder()
                           .id(menu.getId())
                           .label(menu.getName())
                           .build())
                   .collect(Collectors.toList());
       }
       return LabelOptionDTO.builder()
               .id(item.getId())
               .label(item.getName())
               .children(list)
               .build();
   }).collect(Collectors.toList());
   ```



## 资源模块

### 1）查看全部资源列表

#### 参数

keywords关键字，这里用ConditionVo类来封装。

#### 简介

直接通过关键字来模糊查询，如果没有关键字直接返回全部列表

#### 实现细节

1. 依旧是先得到全部资源列表

   ```java
   // 查询资源列表
   List<Resource> resourceList = resourceDao.selectList(new LambdaQueryWrapper<Resource>()
                                                        .like(StringUtils.isNotBlank(conditionVO.getKeywords()), Resource::getResourceName, conditionVO.getKeywords()));
   ```

2. 得到父资源模块列表，直接根据`parentId`判断就行

   ```java
   // 获取所有模块
   List<Resource> parentList = listResourceModule(resourceList);
   
   ......
   
   /**
        * 获取所有资源模块
        *
        * @param resourceList 资源列表
        * @return 资源模块列表
        */
   private List<Resource> listResourceModule(List<Resource> resourceList) {
       return resourceList.stream()
           .filter(item -> Objects.isNull(item.getParentId()))
           .collect(Collectors.toList());
   }
   ```

3. 根据父id分组获得该父模块下的资源列表，用map来装`Map<Integer, List<Resource>> childrenMap`

   ```java
   // 根据父id分组获取模块下的资源
   Map<Integer, List<Resource>> childrenMap = listResourceChildren(resourceList);
   
   
   /**
        * 获取模块下的所有资源
        *
        * @param resourceList 资源列表
        * @return 模块资源
        */
   private Map<Integer, List<Resource>> listResourceChildren(List<Resource> resourceList) {
       return resourceList.stream()
           .filter(item -> Objects.nonNull(item.getParentId()))
           .collect(Collectors.groupingBy(Resource::getParentId));
   }
   ```

4. 将父模块与子模块list绑定：

   ```java
   // 绑定模块下的所有接口
   List<ResourceDTO> resourceDTOList = parentList.stream().map(item -> {
       ResourceDTO resourceDTO = BeanCopyUtils.copyObject(item, ResourceDTO.class);
       List<ResourceDTO> childrenList = BeanCopyUtils.copyList(childrenMap.get(item.getId()), ResourceDTO.class);
       resourceDTO.setChildren(childrenList);
       childrenMap.remove(item.getId());
       return resourceDTO;
   }).collect(Collectors.toList());
   ```

5. 如果有子模块没有分到对应的父模块中去，就直接加在最后一起返回，就如果一个父模块一样

   ```java
   // 若还有资源未取出则拼接
   if (CollectionUtils.isNotEmpty(childrenMap)) {
       List<Resource> childrenList = new ArrayList<>();
       childrenMap.values().forEach(childrenList::addAll);
       List<ResourceDTO> childrenDTOList = childrenList.stream()
           .map(item -> BeanCopyUtils.copyObject(item, ResourceDTO.class))
           .collect(Collectors.toList());
       resourceDTOList.addAll(childrenDTOList);
   }
   ```

#### 注意

这里里面再一次使用了stream来解决父子模块的绑定，很帅气！

### 2）查看全部角色的资源列表

#### 参数

无。

#### 简介

查询全部角色所管理的资源列表，这里直接返回全部就行，其实和上面第一个接口有点类似，只是这里查询字段减少，加快了查询速度。

#### 实现细节

1. 其实和后台菜单模块中的第五个接口有点像：依旧是先得到主目录和对应子目录，然后进行拼接操作。这里用mapper层的select方法方便快捷的查询这三个字段的数据：

   ```java
   // 查询资源列表
   List<Resource> resourceList = resourceDao.selectList(new LambdaQueryWrapper<Resource>()
                                 .select(Resource::getId, Resource::getResourceName, Resource::getParentId)
                                 .eq(Resource::getIsAnonymous, FALSE));
   ```

2. ```java
   // 查询资源列表
   List<Resource> resourceList = resourceDao.selectList(new LambdaQueryWrapper<Resource>()
                                         .select(Resource::getId, Resource::getResourceName, Resource::getParentId)
                                         .eq(Resource::getIsAnonymous, FALSE));
   // 获取所有模块
   List<Resource> parentList = listResourceModule(resourceList);
   // 根据父id分组获取模块下的资源
   Map<Integer, List<Resource>> childrenMap = listResourceChildren(resourceList);
   // 组装父子数据
   return parentList.stream().map(item -> {
       List<LabelOptionDTO> list = new ArrayList<>();
       List<Resource> children = childrenMap.get(item.getId());
       if (CollectionUtils.isNotEmpty(children)) {
           list = children.stream()
               .map(resource -> LabelOptionDTO.builder()
                    .id(resource.getId())
                    .label(resource.getResourceName())
                    .build())
               .collect(Collectors.toList());
       }
       return LabelOptionDTO.builder()
           .id(item.getId())
           .label(item.getResourceName())
           .children(list)
           .build();
   }).collect(Collectors.toList());
   ```

### 3）新增或修改资源

#### 参数

```java
{
  "id": {非必须},
  "isAnonymous": {非必须，有默认值},
  "parentId": {},
  "requestMethod": {},
  "resourceName": {},
  "url": {}
}
```

#### 简介

通过传入一个resource对象，新增或修改资源。

#### 实现细节

1. 先更新资源信息

   ```java
   // 更新资源信息
   Resource resource = BeanCopyUtils.copyObject(resourceVO, Resource.class);
   this.saveOrUpdate(resource);
   ```

2. 重新加载角色资源信息到服务器中，方便下次请求时能正确鉴权

   ```java
   // 重新加载角色资源信息到服务器中
   mySourceSafilterAuthStrategy.clearDataSource();
   ```

### 4）新增或修改资源

#### 参数

资源id就行

#### 简介

通过资源id删除，同时有子资源的话要删除全部子资源。

#### 实现细节

1. 先通过**角色—资源**表查询是否有角色和该资源关联：

   ```java
   Integer count = roleResourceDao.selectCount(new LambdaQueryWrapper<RoleResource>()
                                               .eq(RoleResource::getResourceId, resourceId));
   if (count > 0) {
       throw new AppException("该资源下存在角色");
   }
   ```

2. 构建一个`List<Integer> resourceIdList`链表来装需要删除的资源id，其中包括子资源：

   ```java
   // 删除子资源
   List<Integer> resourceIdList = resourceDao.selectList(new LambdaQueryWrapper<Resource>()
                                                         .select(Resource::getId).
                                                         eq(Resource::getParentId, resourceId))
       .stream()
       .map(Resource::getId)
       .collect(Collectors.toList());
   resourceIdList.add(resourceId);
   resourceDao.deleteBatchIds(resourceIdList);
   ```

   

## 用户角色模块

### 1）查询全部用户角色

#### 参数

无。

#### 简介

直接查询role表，得到全部的角色+角色id，这个接口主要用于给用户添加角色时查询roleId用的。

#### 实现细节

1. 直接查询id和roleName

   ```java
   // 查询角色列表
   List<Role> roleList = roleMapper.selectList(new LambdaQueryWrapper<Role>()
                                               .select(Role::getId, Role::getRoleName));
   return BeanCopyUtils.copyList(roleList, UserRoleDTO.class);
   ```

2. 依旧巧妙地使用了`BeanCopyUtils.copyList`。

### 2）分页查询角色列表

#### 参数

封装在ConditionVO中的current和size

#### 简介

通过传入当前页和页面大小，来进行分页查询

#### 实现细节

1. 查询角色列表，这里可以直接查询到每一个角色所拥有的资源和菜单列表：这里通过mapper层自定义的方法`listRoles`来实现

   ```java
   // 查询角色列表，这里可以直接查询到每一个角色所拥有的资源和菜单列表
   List<RoleDTO> roleDTOList = roleMapper.listRoles(PageUtils.getLimitCurrent(), PageUtils.getSize(), conditionVO);
   ```

   mapper层：

   ```xml
   <resultMap id="RoleMap" type="org.cuit.epoch.dto.RoleDTO">
       <id column="id" property="id"/>
       <result column="role_name" property="roleName"/>
       <result column="role_label" property="roleLabel"/>
       <result column="create_time" property="createTime"/>
       <result column="is_disable" property="isDisable"/>
       <collection property="resourceIdList" ofType="java.lang.Integer">
       <constructor>
       <arg column="resource_id"/>
       </constructor>
       </collection>
       <collection property="menuIdList" ofType="java.lang.Integer">
       <constructor>
       <arg column="menu_id"/>
       </constructor>
       </collection>
       
   </resultMap>
   <select id="listRoles" resultMap="RoleMap">
           SELECT
           r.id,
           role_name,
           role_label,
           r.create_time,
           r.is_disable,
           rr.resource_id,
           rm.menu_id
           FROM
           (
           SELECT
           id,
           role_name,
           role_label,
           create_time,
           is_disable
           FROM
           tb_role
           <where>
               <if test="conditionVO.keywords != null ">
                   role_name like concat('%',#{conditionVO.keywords},'%')
               </if>
           </where>
           LIMIT #{current}, #{size}
           ) r
           LEFT JOIN tb_role_resource rr ON r.id = rr.role_id
           LEFT JOIN tb_role_menu rm on r.id = rm.role_id
           ORDER BY r.id
   </select>
   ```

#### 注意

值得提一嘴的是，这个接口是查询到每一个角色所拥有的资源和菜单列表，需要配合资源模块中的`查询全部角色的资源列表`和菜单模块中的`查询全部角色菜单列表`使用，这样才能达到前端展示的效果![image-20221128142429742](https://figurebed-ladidol.oss-cn-chengdu.aliyuncs.com/img/202211281424020.png)



### 3）保存或更新角色信息

#### 参数

```json
{
  "id": {保存操作时可以为null，更新操作的时候这里就需要id不为空了},
  "menuIdList": {需要添加或者更新的菜单id列表},
  "resourceIdList": {需要更新的菜单列表},
  "roleLabel": {标签},
  "roleName": {角色名}
}
```



#### 简介

保存和更新角色信息共用的一个接口。

#### 实现细节

1. 先判断用户名是不是重复，**值得注意**的是如果是第一次保存角色的话，roleName相同，就会报错，如果是更改角色信息的话，这里因为id相同而不会报错！

   ```java
   // 判断角色名重复
   Role existRole = roleMapper.selectOne(new LambdaQueryWrapper<Role>()
                                         .select(Role::getId)
                                         .eq(Role::getRoleName, roleVO.getRoleName()));
   if (Objects.nonNull(existRole) && !existRole.getId().equals(roleVO.getId())) {
       throw new AppException("角色名已存在");
   }
   ```

2. 构建一个entity对象，保存或者更新角色信息：

   ```java
   // 保存或更新角色信息
   Role role = Role.builder()
       .id(roleVO.getId())
       .roleName(roleVO.getRoleName())
       .roleLabel(roleVO.getRoleLabel())
       .isDisable(CommonConst.FALSE)
       .build();
   this.saveOrUpdate(role);//自动填充roleId
   ```

3. 对于resourceIdList和menusIdList，我们需要分别更新角色绑定表；

4. 更新角色资源表：先删除该角色的全部资源绑定---》再添加该角色的资源绑定

   ```java
   // 更新角色资源关系
   if (Objects.nonNull(roleVO.getResourceIdList())) {
       //先删除表中关于这个角色的全部资源绑定
       if (Objects.nonNull(roleVO.getId())) {
           roleResourceService.remove(new LambdaQueryWrapper<RoleResource>()
                                      .eq(RoleResource::getRoleId, roleVO.getId()));
       }
       //再添加这个角色的资源绑定
       List<RoleResource> roleResourceList = roleVO.getResourceIdList().stream()
           .map(resourceId -> RoleResource.builder()
                .roleId(role.getId())
                .resourceId(resourceId)
                .build())
           .collect(Collectors.toList());
       roleResourceService.saveBatch(roleResourceList);
       // 重新加载角色资源信息
       mySourceSafilterAuthStrategy.clearDataSource();
   }
   ```

5. 更新角色菜单表：先删除该角色的全部菜单绑定---》再添加该角色的菜单绑定

   ```java
   // 更新角色菜单关系
   if (Objects.nonNull(roleVO.getMenuIdList())) {
       if (Objects.nonNull(roleVO.getId())) {
           roleMenuService.remove(new LambdaQueryWrapper<RoleMenu>().eq(RoleMenu::getRoleId, roleVO.getId()));
       }
       List<RoleMenu> roleMenuList = roleVO.getMenuIdList().stream()
           .map(menuId -> RoleMenu.builder()
                .roleId(role.getId())
                .menuId(menuId)
                .build())
           .collect(Collectors.toList());
       roleMenuService.saveBatch(roleMenuList);
   }
   ```

6. 这个接口的操作我们需要用日志来记录，这里通过自定义注解`@OptLog`来

   ```java
   @OptLog(optType = SAVE_OR_UPDATE)
   @ApiOperation(value = "保存或更新角色")
   @PostMapping("/admin/role")
   public Result<?> saveOrUpdateRole(@RequestBody @Valid RoleVO roleVO) {
       roleService.saveOrUpdateRole(roleVO);
       return Result.ok();
   }
   ```

   

#### 注意

1. 依旧是使用流来代替for循环操作
2. 这里对于资源绑定的不同，我们没有直接对比添加，而是先删除以前的资源绑定，再添加现在的资源绑定，这样想来实现也挺简单的。

### 4）删除角色

#### 参数

传入id链表



#### 简介

前端传入id链表，可以直接进行批量删除

#### 实现细节

1. 先判断该角色下有没有其他用户绑定着

   ```java
   // 判断角色下是否有用户
   Integer count = userRoleDao.selectCount(new LambdaQueryWrapper<UserRole>()
                                           .in(UserRole::getRoleId, roleIdList));
   if (count > 0) {
       throw new AppException("该角色下存在用户");
   }
   ```

2. 直接删除就行啦！

   ```java
   roleMapper.deleteBatchIds(roleIdList);
   ```

3. 该操作也是需要操作日志持久化一下的：`@OptLog(optType = REMOVE)`





## 友链模块

### 1）查看友链列表

#### 参数

无。

#### 简介

不需要分页查询

#### 实现细节

1. 直接通过mp自带selectList查询就行

   ```java
   // 查询友链列表
   List<FriendLink> friendLinkList = friendLinkMapper.selectList(null);
   return BeanCopyUtils.copyList(friendLinkList, FriendLinkDTO.class);
   ```

### 2）查看后台友链列表

#### 参数

当前页码+页面大小 可选的 关键字模糊搜索

#### 简介

可实现的关键字模糊搜索的分页查询

#### 实现细节

1. 先查询出来

   ```java
   // 分页查询友链列表
   Page<FriendLink> page = new Page<>(PageUtils.getCurrent(), PageUtils.getSize());
   Page<FriendLink> friendLinkPage = friendLinkMapper.selectPage(page, new LambdaQueryWrapper<FriendLink>()
                                                                 .like(StringUtils.isNotBlank(condition.getKeywords()), FriendLink::getLinkName, condition.getKeywords()));
   ```

2. 在进行DTO转化就行了

   ```java
   // 转换DTO
   List<FriendLinkBackDTO> friendLinkBackDTOList = BeanCopyUtils.copyList(friendLinkPage.getRecords(), FriendLinkBackDTO.class);
   return new PageResult<>(friendLinkBackDTOList, (int) friendLinkPage.getTotal());
   ```

### 3）保存或更新友链

#### 参数

```java
{
  "id": {不填},
  "linkAddress": {url},
  "linkAvatar": {头像url},
  "linkIntro": {简介},
  "linkName": {网站名}
}
```



#### 简介

简单的保存操作

#### 实现细节

1. ```java
   FriendLink friendLink = BeanCopyUtils.copyObject(friendLinkVO, FriendLink.class);
   this.saveOrUpdate(friendLink);
   ```



### 4）删除友链

#### 参数

友链id

#### 简介

根据友链id直接删除就行

#### 实现细节

1. ```java
   friendLinkService.removeByIds(linkIdList);
   ```




## 文章分类模块

### 1）前台查看分类列表

#### 参数

无。

#### 简介

不需要分页查询

#### 实现细节

1. 直接通过mapper层的自定义sql查询列表就行

   ```java
       <select id="listCategoryDTO" resultType="org.cuit.epoch.dto.category.CategoryDTO">
   		SELECT
   		  c.id,
   		  c.category_name,
   		  COUNT( a.id ) AS article_count
   		FROM
   		  tb_category c
   		  LEFT JOIN ( SELECT id, category_id FROM tb_article WHERE is_delete = 0 AND `status` = 1 ) a ON c.id = a.category_id
   		GROUP BY
   		  c.id
       </select>
   ```

   service层：

   ```java
   return new PageResult<>(categoryMapper.listCategoryDTO(), categoryMapper.selectCount(null));
   ```

   值得提示的是很多查询全部数据或者查询总数，直接存入空(null)的queryWrapper就可以





### 2）后台查看分类列表

#### 参数

size+current+condition

#### 简介

简单的分页查询+模糊查询关键字参数

#### 实现细节

1. 先快速查询一下，判断空否

   ```java
   // 查询分类数量
   Integer count = categoryMapper.selectCount(new LambdaQueryWrapper<Category>()
                                              .like(StringUtils.isNotBlank(condition.getKeywords()), Category::getCategoryName, condition.getKeywords()));
   if (count == 0) {
       return new PageResult<>();
   }
   ```

2. 有数据就开始分页查询

   service层：

   ```java
   // 分页查询分类列表
   List<CategoryBackDTO> categoryList = categoryMapper.listCategoryBackDTO(PageUtils.getLimitCurrent(), PageUtils.getSize(), condition);
   return new PageResult<>(categoryList, count);
   ```

   mapper层：

   ```java
   	<select id="listCategoryBackDTO" resultType="org.cuit.epoch.dto.category.CategoryBackDTO">
   		SELECT
   		  c.id,
   		  c.category_name,
   		  COUNT( a.id ) AS article_count,
   		  c.create_time
   		FROM
   		  tb_category c
   		  LEFT JOIN tb_article a ON c.id = a.category_id
   		<where>
   			<if test="condition.keywords != null">
   			     category_name like concat('%',#{condition.keywords},'%')
   			</if>
   		</where>
   		GROUP BY
   		  c.id
   		ORDER BY
   		  c.id DESC
           LIMIT #{current},#{size}
   	</select>
   ```

#### 注意

1. 注意其中sql中的`concat函数`是用于判断参数是不是是为空用的。同时双重保障

   ```sql
   	<where>
   		<if test="condition.keywords != null">
   		     category_name like concat('%',#{condition.keywords},'%')
   		</if>
   	</where>
   ```

2. 这里直接传了对象condition，进去再用的其属性keywords



### 3）模糊搜索分类（写文章时用的）

#### 参数

可有可无的关键字查询

#### 简介

模糊查询，只查询id和name

#### 实现细节

1. 直接mp中querywrapper查询就行

   ```java
   // 搜索分类
   List<Category> categoryList = categoryMapper.selectList(new LambdaQueryWrapper<Category>()
                                                           .like(StringUtils.isNotBlank(condition.getKeywords()), Category::getCategoryName, condition.getKeywords())
                                                           .orderByDesc(Category::getId));
   return BeanCopyUtils.copyList(categoryList, CategoryOptionDTO.class);
   ```

   







### 4）添加或修改分类

#### 参数

无。

#### 简介

不需要分页查询，需要加到操作日志中去`@OptLog(optType = SAVE_OR_UPDATE)`

#### 实现细节

1. 依旧是用很帅的方式来判断重复（也巧妙地分开了保存和修改操作）

   ```java
   Category existCategory = categoryMapper.selectOne(new LambdaQueryWrapper<Category>()
                                                     .select(Category::getId)
                                                     .eq(Category::getCategoryName, categoryVO.getCategoryName()));
   if (Objects.nonNull(existCategory) && !existCategory.getId().equals(categoryVO.getId())) {
       //依旧用到了，这种方式来分开保存和更新操作的重复判。
       throw new AppException("分类名已存在");
   }
   ```

2. saveOrUpdate()方法

   ```java
   Category category = Category.builder()
       .id(categoryVO.getId())
       .categoryName(categoryVO.getCategoryName())
       .build();
   this.saveOrUpdate(category);
   ```

   



### 5）删除分类

#### 参数

categoryIdList，分类id列表

#### 简介

批量删除文章，其中需要判断分类下有没有其他的文章绑定。需要加到操作日志中去`@OptLog(optType = REMOVE)`

#### 实现细节

1. 判断有没有文章绑定有（这里一个article只有一个category，所以直接用categoryId查询就行咯，所以直接查询Article表就行了），种类用in来判断。

   ```java
   // 查询分类id下是否有文章
   Integer count = articleMapper.selectCount(new LambdaQueryWrapper<Article>()
                                             .in(Article::getCategoryId, categoryIdList));
   if (count > 0) {
       throw new AppException("删除失败，该分类下存在文章");
   }
   ```

   

2. deleteBatchIds

   ```java
   categoryMapper.deleteBatchIds(categoryIdList);
   ```

   







## 文章标签模块

### 1）前台查看标签列表

#### 参数

无。

#### 简介

不需要分页查询，这里因为不需要指定每一个标签下面的文章个数（没必要）所以不需要自定义sql语句

#### 实现细节

1. 通过`selectList`和`BeanCopyUtils.copyList`

   ```java
   // 查询标签列表
   List<Tag> tagList = tagDao.selectList(null);
   // 转换DTO
   List<TagDTO> tagDTOList = BeanCopyUtils.copyList(tagList, TagDTO.class);
   ```

   页面返回

   ```java
   // 查询标签数量
   Integer count = tagDao.selectCount(null);
   return new PageResult<>(tagDTOList, count);
   ```

   



### 2）后台查看标签列表

#### 参数

size+current+condition

#### 简介

简单的分页查询+模糊查询关键字参数

#### 实现细节

1. 先快速查询一下，判断空否

   ```java
   // 查询标签数量
   Integer count = tagDao.selectCount(new LambdaQueryWrapper<Tag>()
                                      .like(StringUtils.isNotBlank(condition.getKeywords()), Tag::getTagName, condition.getKeywords()));
   if (count == 0) {
       return new PageResult<>();
   }
   ```

2. 有数据就开始分页查询

   service层：

   ```java
   // 分页查询标签列表
   List<TagBackDTO> tagList = tagDao.listTagBackDTO(PageUtils.getLimitCurrent(), PageUtils.getSize(), condition);
   return new PageResult<>(tagList, count);
   ```

   mapper层：

   ```java
       <select id="listTagBackDTO" resultType="org.cuit.epoch.dto.tag.TagBackDTO">
           SELECT
             t.id,
             tag_name,
             COUNT( tat.article_id ) AS article_count,
             t.create_time
           FROM
             tb_tag t
             LEFT JOIN tb_article_tag tat ON t.id = tat.tag_id
           <where>
               <if test="condition.keywords != null">
                    tag_name like concat('%',#{condition.keywords},'%')
               </if>
           </where>
           GROUP BY
             t.id
           ORDER BY
             t.id DESC
           LIMIT #{current},#{size}
       </select>
   ```

#### 注意

1. 注意其中sql中的`concat函数`是用于判断参数是不是是为空用的。同时双重保障

   ```sql
           <where>
               <if test="condition.keywords != null">
                    tag_name like concat('%',#{condition.keywords},'%')
               </if>
           </where>
   ```

2. 这里直接传了对象condition，进去再用的其属性keywords

3. 就是和分类的后台列表查询一样的模板



### 3）模糊搜索标签（写文章时用的）

#### 参数

可有可无的关键字查询

#### 简介

模糊查询，只查询id和name

#### 实现细节

1. 直接mp中querywrapper查询就行（id降序，就是添加的顺序）

   ```java
   // 搜索标签
   List<Tag> tagList = tagDao.selectList(new LambdaQueryWrapper<Tag>()
                                         .like(StringUtils.isNotBlank(condition.getKeywords()), Tag::getTagName, condition.getKeywords())
                                         .orderByDesc(Tag::getId));
   return BeanCopyUtils.copyList(tagList, TagDTO.class);
   ```

   







### 4）添加或修改标签

#### 参数

无。

#### 简介

不需要分页查询，需要加到操作日志中去`@OptLog(optType = SAVE_OR_UPDATE)`

#### 实现细节

1. 依旧是用很帅的方式来判断重复（也巧妙地分开了保存和修改操作）

   ```java
   // 查询标签名是否存在
   Tag existTag = tagDao.selectOne(new LambdaQueryWrapper<Tag>()
                                   .select(Tag::getId)
                                   .eq(Tag::getTagName, tagVO.getTagName()));
   if (Objects.nonNull(existTag) && !existTag.getId().equals(tagVO.getId())) {
       throw new AppException("标签名已存在");
   }
   ```

2. saveOrUpdate()方法

   ```java
   Tag tag = BeanCopyUtils.copyObject(tagVO, Tag.class);
   this.saveOrUpdate(tag);
   ```

   



### 5）删除标签

#### 参数

tagIdList，标签id列表

#### 简介

批量删除文章，其中需要判断分类下有没有其他的文章绑定。需要加到操作日志中去`@OptLog(optType = REMOVE)`

#### 实现细节

1. 判断有没有文章绑定有，种类用in来判断。

   ```java
   // 查询标签下是否有文章
   Integer count = articleTagDao.selectCount(new LambdaQueryWrapper<ArticleTag>()
                                             .in(ArticleTag::getTagId, tagIdList));
   if (count > 0) {
       throw new AppException("删除失败，该标签下存在文章");
   }
   ```

   

2. deleteBatchIds

   ```java
   tagDao.deleteBatchIds(tagIdList);
   ```




## 页面模块

### 1）保存或更新页面

#### 参数

```json
{
  "id": {保存默认为空，更新就会带有id},
  "pageCover": {封面url},
  "pageLabel": {页面标签，前端要用的},
  "pageName": {页面名字}
}
```



#### 简介

就直接保存页面对象

#### 实现细节

1. 这里就没有去重判断了

   ```java
   @Transactional(rollbackFor = Exception.class)
   @Override
   public void saveOrUpdatePage(PageVO pageVO) {
       Page page = BeanCopyUtils.copyObject(pageVO, Page.class);
       this.saveOrUpdate(page);
       // 删除缓存
       redisService.del(PAGE_COVER);
   }
   
   ```

2. 为了加快博客访问速度，我们将页面基本信息放到了redis中了，删除了就更新一下redis吧。

### 2）删除用户id

#### 参数

pageId

#### 简介

通过页面id来删除页面

#### 实现细节

1. ```java
   @Transactional(rollbackFor = Exception.class)
   @Override
   public void deletePage(Integer pageId) {
       pageDao.deleteById(pageId);
       // 删除缓存
       redisService.del(PAGE_COVER);
   }
   ```

2. 同样要更新redis



### 3）获取页面列表

#### 参数

无。

#### 简介

不需要分页查询，直接查全部就行

#### 实现细节

1. 查找缓存信息，不存在则从mysql读取，更新缓存

   ```java
   List<PageVO> pageVOList;
   // 查找缓存信息，不存在则从mysql读取，更新缓存
   Object pageList = redisService.get(PAGE_COVER);
   ```

2. 存在缓存：用了alibaba的fastjson，真好

   ```java
   pageVOList = JSON.parseObject(pageList.toString(), List.class);
   ```

3. 不存在缓存：从数据库中拿

   ```java
   pageVOList = BeanCopyUtils.copyList(pageDao.selectList(null), PageVO.class);
   redisService.set(PAGE_COVER, JSON.toJSONString(pageVOList));
   ```




## 日志模块

### 1）查看操作日志

#### 参数

size+current+description_key_word+name_key_word



#### 简介

通过对简介和名字的模糊查询

#### 实现细节

1. 用页面返回

   ```java
   Page<OperationLog> page = new Page<>(PageUtils.getCurrent(), PageUtils.getSize());
   ```

2. 通过mp的service层自带方法进行分页查询

   ```java
   // 查询日志列表
   Page<OperationLog> operationLogPage = this.page(page, new LambdaQueryWrapper<OperationLog>()
                                                   .like(StringUtils.isNotBlank(conditionVO.getKeywords()), OperationLog::getOptModule, conditionVO.getKeywords())
                                                   .or()
                                                   .like(StringUtils.isNotBlank(conditionVO.getKeywords()), OperationLog::getOptDesc, conditionVO.getKeywords())
                                                   .orderByDesc(OperationLog::getId));
   ```

3. 包装返回、

   ```java
   List<OperationLogDTO> operationLogDTOList = BeanCopyUtils.copyList(operationLogPage.getRecords(), OperationLogDTO.class);
   return new PageResult<>(operationLogDTOList, (int) operationLogPage.getTotal());
   ```

### 2）删除日志

#### 参数

日志id链表

#### 简介

就直接保存页面对象

#### 实现细节

1. 直接service层的removeByIds

   ```java
   @ApiOperation(value = "删除操作日志")
   @DeleteMapping("/admin/operation/logs")
   public Result<?> deleteOperationLogs(@RequestBody List<Integer> logIdList) {
       operationLogService.removeByIds(logIdList);
       return Result.ok();
   }
   ```



## 相册模块

### 1）保存或更新相册

#### 参数

```java
{
  "albumCover": {相册封面url},
  "albumDesc": {相册简介},
  "albumName": {相册名字},
  "id": {id},
  "status": {是否隐藏}
}
```

#### 简介

保存or更新相册信息，这里的处理依旧是需要判断重复性，用了前面都用过的方法

#### 实现细节

1. 通过查询判断项目名是否已经存在

   ```java
   PhotoAlbum album = photoAlbumDao.selectOne(new LambdaQueryWrapper<PhotoAlbum>()
                                              .select(PhotoAlbum::getId)
                                              .eq(PhotoAlbum::getAlbumName, photoAlbumVO.getAlbumName()));
   if (Objects.nonNull(album) && !album.getId().equals(photoAlbumVO.getId())) {
       throw new AppException("相册名已存在");
   }
   ```

2. 然后调用saveOrUpdate方法，万能

   ```java
   PhotoAlbum photoAlbum = BeanCopyUtils.copyObject(photoAlbumVO, PhotoAlbum.class);
   this.saveOrUpdate(photoAlbum);
   ```



### 2）上传相册封面

#### 参数

MultipartFile文件

#### 简介

上传文件，返回给前端url，方便前端调用`保存更新相册`接口

#### 实现细节

1. 直接调用上传策略上下文中的上传方法，同时标记这是PHOTO模块里面的的图片

   ```java
   return Result.ok(uploadStrategyContext.executeUploadStrategy(file, FilePathEnum.PHOTO.getPath()));
   ```





### 3）后台获取相册列表信息

#### 参数

无参数

#### 简介

后台查看相册列表，就直接将全部信息返给前段就行。简单实现。

#### 实现细节

1. ```java
   List<PhotoAlbum> photoAlbumList = photoAlbumDao.selectList(new LambdaQueryWrapper<PhotoAlbum>()
                                                              .eq(PhotoAlbum::getIsDelete, FALSE));
   return BeanCopyUtils.copyList(photoAlbumList, PhotoAlbumDTO.class);
   ```



### 4）后台根据id获取相册详细信息

#### 参数

albumId相册id

#### 简介

后台查看相册指定相册的详细信息，包括照片数量。

#### 实现细节

1. 查询相册信息

   ```java
   PhotoAlbum photoAlbum = photoAlbumDao.selectById(albumId);
   ```

2. 查询照片数量（这里用到了照片模块的部分方法）

   ```java
   // 查询照片数量
   Integer photoCount = photoDao.selectCount(new LambdaQueryWrapper<Photo>()
                                             .eq(Photo::getAlbumId, albumId)
                                             .eq(Photo::getIsDelete, FALSE));
   PhotoAlbumBackDTO album = BeanCopyUtils.copyObject(photoAlbum, PhotoAlbumBackDTO.class);
   ```

3. 更新album中的photoCount字段

   ```java
   album.setPhotoCount(photoCount);
   return album;
   ```



### 5）根据id删除相册

#### 参数

albumId相册id

#### 简介

通过id删除相册

#### 实现细节

1. 先看看相册下面有没有照片

   ```java
   // 查询照片数量
   Integer count = photoDao.selectCount(new LambdaQueryWrapper<Photo>()
                                        .eq(Photo::getAlbumId, albumId));
   ```

2. 如果`count>0`则

   ```java
   // 若相册下存在照片则逻辑删除相册
   photoAlbumDao.updateById(PhotoAlbum.builder()
                            .id(albumId)
                            .isDelete(TRUE)
                            .build());
   // 再删除照片
   photoDao.update(new Photo(), new LambdaUpdateWrapper<Photo>()
                   .set(Photo::getIsDelete, TRUE)
                   .eq(Photo::getAlbumId, albumId));
   ```

3. 否则

   ```java
   // 若相册下不存在照片则直接删除
   photoAlbumDao.deleteById(albumId);
   ```





### 6）前台获取相册列表

#### 参数

无

#### 简介

前台获取公开相册列表，根据id排序

#### 实现细节

1. ```java
   // 查询相册列表
   List<PhotoAlbum> photoAlbumList = photoAlbumDao.selectList(new LambdaQueryWrapper<PhotoAlbum>()
                                                              .eq(PhotoAlbum::getStatus, PUBLIC.getStatus())
                                                              .eq(PhotoAlbum::getIsDelete, FALSE)
                                                              .orderByDesc(PhotoAlbum::getId));
   return BeanCopyUtils.copyList(photoAlbumList, PhotoAlbumDTO.class);
   ```





## 照片模块

### 1）后台根据相册id获取照片列表

#### 参数

```java
{
  "albumId": {相册封面url},
  "isDelete": {是否已经删除的}
}
```

#### 简介

通过相册id查询隶属于它的照片，同时根据isDelete属性来判断查询`未删除的照片`or`逻辑删除的照片`

#### 实现细节

1. 直接selectPage查询

   ```java
   // 查询照片列表
   Page<Photo> page = new Page<>(PageUtils.getCurrent(), PageUtils.getSize());
   Page<Photo> photoPage = photoDao.selectPage(page, new LambdaQueryWrapper<Photo>()
                                               .eq(Objects.nonNull(condition.getAlbumId()), Photo::getAlbumId, condition.getAlbumId())
                                               .eq(Photo::getIsDelete, condition.getIsDelete())
                                               .orderByDesc(Photo::getId)
                                               .orderByDesc(Photo::getUpdateTime));
   ```

2. 封装返回

   ```java
   List<PhotoBackDTO> photoList = BeanCopyUtils.copyList(photoPage.getRecords(), PhotoBackDTO.class);
   return new PageResult<>(photoList, (int) photoPage.getTotal());
   ```



### 2）更新照片信息

#### 参数

```java
{
  "albumId": {相册封面url},
  "isDelete": {是否已经删除的}
}
```

#### 简介

根据照片id更新就行

#### 实现细节

1. 根据id更新信息就行了

   ```java
   Photo photo = BeanCopyUtils.copyObject(photoInfoVO, Photo.class);
   photoDao.updateById(photo);
   ```

   



### 3）保存照片信息

#### 参数

```java
{
  "albumId": {保存于的相册id},
  "photoIdList": {保存操作是为空的},
  "photoUrlList": {url集合}
}
```

#### 简介

通过相册id保存全部url于其麾下

#### 实现细节

1. 通过stream操作，将每一个url分别用一个photo对象来装，同时赋值同样的albumId

   ```java
   List<Photo> photoList = photoVO.getPhotoUrlList().stream().map(item -> Photo.builder()
                                                                  .albumId(photoVO.getAlbumId())
                                                                  .photoName(IdWorker.getIdStr())
                                                                  .photoSrc(item)
                                                                  .build())
       .collect(Collectors.toList());
   ```

2. 然后调用批量插入`this.saveBatch(photoList);`

   ```java
   this.saveBatch(photoList);
   ```



### 4）移动照片到指定相册

#### 参数

```java
{
  "albumId": {目的相册id},
  "photoIdList": {更新操作存在id},
  "photoUrlList": {是不用传的}
}
```

#### 简介

就是通过photoid修改每一个id的albumId就行勒

#### 实现细节

1. 为每一个photo对象赋值新的albumId

   ```java
   List<Photo> photoList = photoVO.getPhotoIdList().stream().map(item -> Photo.builder()
                                                                 .id(item)
                                                                 .albumId(photoVO.getAlbumId())
                                                                 .build())
       .collect(Collectors.toList());
   ```

2. 根据id批量插入

   ```java
   this.updateBatchById(photoList);
   ```

   





### 5）更新照片删除状态（逻辑删除和恢复）

#### 参数

```java
{
  "idList": {需要修改的照片id},
  "isDelete": {删除：1，恢复：0}
}
```

#### 简介

根据照片ID进行删除或者恢复。如果是恢复操作，需要同时判断相册需不需要恢复！！！

#### 实现细节

1. 先更新每个照片的状态

   ```java
   // 更新照片状态
   List<Photo> photoList = deleteVO.getIdList().stream().map(item -> Photo.builder()
   .id(item)
   .isDelete(deleteVO.getIsDelete())
   .build())
   .collect(Collectors.toList());
   this.updateBatchById(photoList);
   ```

2. 判断是不是恢复操作，如果是就开始判断相册是不是需要恢复

   ```java
   // 若恢复照片所在的相册已删除，恢复相册
   if (deleteVO.getIsDelete().equals(FALSE)) {
       List<PhotoAlbum> photoAlbumList = photoDao.selectList(new LambdaQueryWrapper<Photo>()
                                                             .select(Photo::getAlbumId)
                                                             .in(Photo::getId, deleteVO.getIdList())
                                                             .groupBy(Photo::getAlbumId))
           .stream()
           .map(item -> PhotoAlbum.builder()
                .id(item.getAlbumId())
                .isDelete(FALSE)
                .build())
           .collect(Collectors.toList());
       photoAlbumService.updateBatchById(photoAlbumList);
   }
   ```

   



### 6）删除照片

#### 参数

photoIdList集合

#### 简介

直接通过photoId集合将回收站中图片直接物理删除

#### 实现细节

1. ```java
   photoDao.deleteBatchIds(photoIdList);
   ```

   



### 7）前台根据相册id查看照片列表

#### 参数

id

#### 简介

通过albumId查询照片列表

#### 实现细节

1. 先判断相册是不是存在

   ```java
   // 查询相册信息
   PhotoAlbum photoAlbum = photoAlbumService.getOne(new LambdaQueryWrapper<PhotoAlbum>()
                                                    .eq(PhotoAlbum::getId, albumId)
                                                    .eq(PhotoAlbum::getIsDelete, FALSE)
                                                    .eq(PhotoAlbum::getStatus, PUBLIC.getStatus()));
   if (Objects.isNull(photoAlbum)) {
       throw new AppException("相册不存在");
   }
   ```

2. 只查询照片url，同时封装相册名字相册图片ulr和全部图片的url集合

   ```java
   // 查询照片列表
   Page<Photo> page = new Page<>(PageUtils.getCurrent(), PageUtils.getSize());
   List<String> photoList = photoDao.selectPage(page, new LambdaQueryWrapper<Photo>()
                                                .select(Photo::getPhotoSrc)
                                                .eq(Photo::getAlbumId, albumId)
                                                .eq(Photo::getIsDelete, FALSE)
                                                .orderByDesc(Photo::getId))
       .getRecords()
       .stream()
       .map(Photo::getPhotoSrc)
       .collect(Collectors.toList());
   return PhotoDTO.builder()
       .photoAlbumCover(photoAlbum.getAlbumCover())
       .photoAlbumName(photoAlbum.getAlbumName())
       .photoList(photoList)
       .build();
   ```


## 说说模块

### 1）查看首页说说（滚动展示）

#### 参数

无参数，默认请求十条说说数据

#### 简介

根据置顶、创建顺序返回前十个。

#### 实现细节

1. 直接查询selectList，很巧妙地是：istop字段1 or 0，直接降序排序就行，id降序也就代表最近创建。同时用到了mp的last方法：在最后添加了限制条数的条件。

   ```java
   // 查询最新10条说说
   List<Talk> talks = talkDao.selectList(new LambdaQueryWrapper<Talk>()
                              .eq(Talk::getStatus, TalkStatusEnum.PUBLIC.getStatus())
                              .orderByDesc(Talk::getIsTop)
                              .orderByDesc(Talk::getId)
                              .last("limit 10"));
   ```

   这段代码sql等价于

   ```sql
   SELECT id,user_id,content,images,is_top,status,create_time,update_time 
   FROM tb_talk 
   WHERE (status = ?) 
   ORDER BY is_top 
   DESC,id DESC 
   limit 10
   ```

2. 依旧是用stream操作来取代for循环，同时进行前200字节截取操作。

   ```java
   return  talks
       .stream()
       .map(item -> item.getContent().length() > 200 ? HTMLUtils.deleteHMTLTag(item.getContent().substring(0, 200)) : HTMLUtils.deleteHMTLTag(item.getContent()))
       .collect(Collectors.toList());
   ```

   

### 2）查看说说链表 todo等评论模块写好了再弄一下吧。

#### 参数

无参数

#### 简介

通过相册id查询隶属于它的照片，同时根据isDelete属性来判断查询`未删除的照片`or`逻辑删除的照片`

#### 实现细节

1. 直接selectPage查询







### 3）根据id查看说说

#### 参数

talkId

#### 简介

通过说说id，前台查看说说的基本信息

#### 实现细节

1. 依旧是常见的判空操作

   ```java
   // 查询说说信息
   TalkDTO talkDTO = talkDao.getTalkById(talkId);
   if (Objects.isNull(talkDTO)) {
       throw new AppException("说说不存在");
   }
   ```

   mapper层

   ```xml
       <select id="getTalkById" resultType="org.cuit.epoch.dto.talk.TalkDTO">
           SELECT
               t.id,
               nickname,
               avatar,
               content,
               images,
               t.create_time
           FROM
               tb_talk t
                   JOIN tb_user_info ui ON t.user_id = ui.id
           WHERE
               t.id = #{talkId}
             AND t.status = 1
       </select>
   ```

   

2. 查看redis中该条说说的点赞数

   ```java
   // 查询说说点赞量
   talkDTO.setLikeCount((Integer) redisService.hGet(TALK_LIKE_COUNT, talkId.toString()));
   ```

3. 转换图片url字段格式，因为存入数据库中的是`[url1,url2,url3]`的string形式，我们前端需要的到的url链表

   ```java
   // 转换图片格式
   if (Objects.nonNull(talkDTO.getImages())) {
       talkDTO.setImgList(
           CommonUtils.castList(
               //先解析成objectList，然后再转化成StringList
               JSON.parseObject(talkDTO.getImages(), List.class),
               String.class)
       );
   }
   ```

   



### 4）点赞说说

#### 参数

talkId

#### 简介

通过talkId和登录用户loginId，将该角色点赞说说情况放到redis中存储

![image-20221213005852699](https://figurebed-ladidol.oss-cn-chengdu.aliyuncs.com/img/202212130127896.png)

#### 实现细节

1. 判断该用户对于该说说是否已经点赞过了

   ```java
   // 判断是否点赞
   String talkLikeKey = TALK_USER_LIKE + StpUtil.getLoginIdAsInt();
   if (redisService.sIsMember(talkLikeKey, talkId)) {
       // 点过赞则删除说说id
       redisService.sRemove(talkLikeKey, talkId);
       // 说说点赞量-1
       redisService.hDecr(TALK_LIKE_COUNT, talkId.toString(), 1L);
   } else {
       // 未点赞则增加说说id
       redisService.sAdd(talkLikeKey, talkId);
       // 说说点赞量+1
       redisService.hIncr(TALK_LIKE_COUNT, talkId.toString(), 1L);
   }
   ```

   



### 5）上传说说图片

#### 参数

图片文件

#### 简介

传入图片文件，直接通过上传策略上传文件并返回url就行了

#### 实现细节

1. ```java
   return Result.ok(uploadStrategyContext.executeUploadStrategy(file, FilePathEnum.TALK.getPath()));
   ```







### 6）保存修改说说

#### 参数

```java
{
  "content": "说说内容",
  "id": 说说id,
  "images": "[图片url1，url2，url3，url4]",
  "isTop": 1是置顶，0不是置顶,
  "status": 1.公开 2.私密
}
```

#### 简介

登录用户保存自己的说说，但是只能是后台才能使用。

#### 实现细节

1. 说说内容可以重复，所以直接用MP的service层的方法就可以

   ```java
   Talk talk = BeanCopyUtils.copyObject(talkVO, Talk.class);
   talk.setUserId(StpUtil.getLoginIdAsInt());
   this.saveOrUpdate(talk);
   ```

   







### 7）删除说说

#### 参数

talkIdList

#### 简介

通过id列表批量删除

#### 实现细节

1. 直接批量删除就行

   ```java
   talkDao.deleteBatchIds(talkIdList);
   ```

   







### 8）查看后台说说列表

#### 参数

size+current+公开还是私密状态情况status

#### 简介

通过关键字进行分页查询+模糊查询

#### 实现细节

1. 可以判断一下是不是没有说说

   ```java
   // 查询说说总量
   Integer count = talkDao.selectCount(new LambdaQueryWrapper<Talk>()
                                       .eq(Objects.nonNull(conditionVO.getStatus()), Talk::getStatus, conditionVO.getStatus()));
   if (count == 0) {
       return new PageResult<>();
   }
   ```

2. 依旧直接通过自定义sql方法访问就行了

   ```java
   // 分页查询说说
   List<TalkBackDTO> talkDTOList = talkDao.listBackTalks(PageUtils.getLimitCurrent(), PageUtils.getSize(), conditionVO);
   ```

   mapper层

   ```xml
       <select id="listBackTalks" resultType="org.cuit.epoch.dto.talk.TalkBackDTO">
           SELECT
           t.id,
           nickname,
           avatar,
           content,
           images,
           t.is_top,
           t.status,
           t.create_time
           FROM
           tb_talk t
           JOIN tb_user_info ui ON t.user_id = ui.id
           <where>
               <if test="condition.status != null">
                   t.status = #{condition.status}
               </if>
           </where>
           ORDER BY
           t.is_top DESC,
           t.id DESC
           LIMIT #{current},#{size}
       </select>
   ```

3. 然后就是转换图片格式

   ```java
   talkDTOList.forEach(item -> {
       // 转换图片格式
       if (Objects.nonNull(item.getImages())) {
           item.setImgList(CommonUtils.castList(JSON.parseObject(item.getImages(), List.class), String.class));
       }
   });
   return new PageResult<>(talkDTOList, count);
   ```



### 9）根据id查看后台说说

#### 参数

talkId

#### 简介

通过talkId进行后台说说详细信息查询

#### 实现细节

1. 通过maper层自定的sql方法查询该说说的详细信息

   ```java
   TalkBackDTO talkBackDTO = talkDao.getBackTalkById(talkId);
   ```

   mapper层

   ```xml
       <select id="getBackTalkById" resultType="org.cuit.epoch.dto.talk.TalkBackDTO">
           SELECT
               t.id,
               nickname,
               avatar,
               content,
               images,
               t.is_top,
               t.status,
               t.create_time
           FROM
               tb_talk t
                   JOIN tb_user_info ui ON t.user_id = ui.id
           WHERE
               t.id = #{talkId}
       </select>
   ```

2. 然后依旧是将String字段的images，转化成链表

   ```java
   // 转换图片格式
   if (Objects.nonNull(talkBackDTO.getImages())) {
       talkBackDTO.setImgList(CommonUtils.castList(JSON.parseObject(talkBackDTO.getImages(), List.class), String.class));
   }
   return talkBackDTO;
   ```















