package org.cuit.epoch.todolist;

/**
 * @author: ladidol
 * @date: 2022/11/16 0:03
 * @description:
 */
public class todolist {
    // TODO: 2022/11/15 给登录的人自然放上数据库中查询到的权限
    // TODO: 2022/11/15 看一下是怎么查询的用户权限，这里就可以吧权限赋值到session中去？解决：通过StpInterfaceImpl中的getRole方法，从数据库中拿到当前用户的角色。
    // TODO: 2022/11/17 怎么用sa-token来做角色管理和资源权限管理，
    // TODO: 2022/11/17 hexo也对后台目录进行了管理真的屌，和前端商量通过component来管理组件。 这样可以通过不同的角色来展示不同的后台列表。
    
    
    // 2022/11/16 登录的时候，先弄成明文，这里的加密好像出问题了。解决：导入加密包就行。
    // 2022/11/17 把密码加密用SpringSecurity的加密先用到起，因为后续需要和security_hexo_blog项目一起共用一个数据库，或者把SpringSecurity项目中的加密用PasswordUtil来做。
    // 2022/11/17 重写一下Security的passwordEcoder方法。bingo


    // 接口文档的实现
    // TODO: 2022/11/16 看一下登录获取登陆者的详细信息是怎么弄的呢？比如各种点赞是从redis中拿取，登录地点是怎么获取的？
    // TODO: 2022/11/16 可以好好看一下首页的实现，比如每个页面的一些基本信息都是保存在redis中，这样查询能快不少。


}