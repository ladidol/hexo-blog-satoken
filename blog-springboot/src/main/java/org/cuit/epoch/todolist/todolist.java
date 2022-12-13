package org.cuit.epoch.todolist;

import cn.dev33.satoken.stp.StpUtil;
import org.cuit.epoch.strategy.UploadStrategy;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.Map;

/**
 * @author: ladidol
 * @date: 2022/11/16 0:03
 * @description:
 */
public class todolist {


    //=====================================================用户模块============================================================================

    // 2022/11/15 给登录的人自然放上数据库中查询到的权限
    // 2022/11/15 看一下是怎么查询的用户权限，这里就可以吧权限赋值到session中去？解决：通过StpInterfaceImpl中的getRole方法，从数据库中拿到当前用户的角色。
    // 2022/11/17 怎么用sa-token来做角色管理和资源权限管理，就是用老项目中的对每一个路由进行判断鉴权。
    // TODO: 2022/11/17 hexo也对后台目录进行了管理真的屌，和前端商量通过component来管理组件。 这样可以通过不同的角色来展示不同的后台列表。

    // 2022/11/18 重构系统准备把登录用户的角色列表放到redis中存储，然后：每一次更新当前用户的角色信息都需要从mysql中更新一下redis，其他情况获取角色信息都是通过redis来直接获取。
    // 2022/11/18 把之前刘涵铭给的那个页面弄一个分支保存（那个index我就够用了）
    // 2022/11/18  用户每次登录记得更新一下最近登录时间.解决！

    //用户信息
    // 2022/11/20  就是UserUtils工具类直接获取当前登录者的全部信息。顺便看一下哪些地方有没有重复。
    // 2022/11/21 又出现了一些问题：就是userInfoId和userAuthId是不一样的，比如updateUserInfo里面传的参数应该是infoid。解决通过存入redis中共的用户信息获取infoId
    @Autowired
    private Map<String, UploadStrategy> uploadStrategyMap;
    // 在spring中，根据spring的特性，Spring 会在启动时，自动查找实现了该接口的 bean，放到这个Map中去。key为bean的名字，value为 实现了该接口的所有的 bean。通过这种方式，就可以不用再通过构造方法将实现的策略类传入
    //2022/11/21 这里感觉好帅啊 ，这个策略模式，UploadStrategy实现类通过@Service("localUploadStrategyImpl")，就能直接用Map来获取。
    //用户redis信息添加
    //redisService.set(USER_INFO + StpUtil.getLoginId(),userDetailDTO);
    //2022/11/21 这里的userDetailsDTO存入redis中共报错， Could not write JSON: Java 8 date/time type `java.time.LocalDateTime` 。解决：    @JsonDeserialize(using = LocalDateTimeDeserializer.class)
    //    @JsonSerialize(using = LocalDateTimeSerializer.class)
    //    private LocalDateTime lastLoginTime;

    // 登录
    // 2022/11/16 登录的时候，先弄成明文，这里的加密好像出问题了。解决：导入加密包就行。
    // 2022/11/17 把密码加密用SpringSecurity的加密先用到起，因为后续需要和security_hexo_blog项目一起共用一个数据库，或者把SpringSecurity项目中的加密用PasswordUtil来做。
    // 2022/11/17 重写一下Security的passwordEcoder方法。bingo
    // TODO: 2022/11/22 这里可以将登录信息放到Sa-token中管理，原先的登录和第三方登录都可以
    // TODO: 2022/11/25 需要根据这个博客来改一下vue前端中的一些配置https://www.talkxj.com/articles/3 ,/public/index.html中
    // TODO: 2022/11/29 后台非admin登录前端弹窗报错信息不正确（没有正确显示）



    //=====================================================博客信息模块============================================================================
    // TODO: 2022/11/24 我在想，博客的访问量是怎么更新到redis中去的了，还有用户的访问量是怎么更新到redis中去的呢


    //=====================================================项目框架的部分知识点============================================================================
    // TODO: 2022/11/25 一些错误的报错的返回

    //=====================================================说说部分============================================================================
    // TODO: 2022/12/14 还剩一个前台查看说说列表的接口，这个接口需要查询评论


    //=====================================================接口文档的实现============================================================================
    // TODO: 2022/11/23 接口文档可以根据 knife4j 的文档进行下载就行了，然后再md文档文档中进行编辑补充详细信息。
    // TODO: 2022/11/16 看一下登录获取登陆者的详细信息是怎么弄的呢？比如各种点赞是从redis中拿取，登录地点是怎么获取的？
    // TODO: 2022/11/16 可以好好看一下首页的实现，比如每个页面的一些基本信息都是保存在redis中，这样查询能快不少。
    // TODO: 2022/11/21 可以试一下github的cicd，或者自己搭建一个gitlab
    // TODO: 2022/11/23 把一些类的简介好好写一下。
    // TODO: 2022/11/25 @ApiModelProperty(name = "name", value = "菜单名", dataType = "String",required = true) 给一些类属性上面都添加一下required = true吧。


    //=====================================================待学的部分知识点============================================================================
    // TODO: 2022/11/25 无聊可以把Stream流部分好好补一下，比如下面对菜单进行分类传给前端就很帅！！！！
    // TODO: 2022/11/25 需要将博客换成https，有空的话 
    
    






}