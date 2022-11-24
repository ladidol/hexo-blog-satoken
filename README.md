# hexo-blog-satoken
重构博客

这里先简单的介绍一下每一个接口的实现，后面通过swagger来md导出，再将这些加进去。



## 角色权限管理模块

### 路由管理

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

### 角色管理

每一次接口的角色信息更新都会调一下`clearDataSource`清空`resourceRoleList`中的数据，同时下次接口访问的时候会调用`loadDataSource`将`resourceRoleList`重新加载





## 登录模块

### 1）邮箱密码登录

#### 参数

参数是email+password

#### 简介

用户通过数据库中已经有的邮箱账号+密码来登录。

#### 实现细节

先查询账号是不是存在的（账号合理性）

这里通过Satoken登录就直接调用`StpUtil.login()`登录就行，同时将用户详细信息存入session，这里也可以考略将他们存入Redis中去。

```java
//sa-token登录
StpUtil.login(userDetailDTO.getId());
//将用户角色信息存入session中
StpUtil.getSession().set(USER_ROLE,userDetailDTO.getRoleList());
//将用户详细信息存入session中
StpUtil.getSession().set(USER_INFO,userDetailDTO);
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

### 4）QQ第三方登录

#### 参数

通过QQ登录得到的openId

#### 简介

携带QQ登录的到的openId访问该接口

#### 实现细节

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





