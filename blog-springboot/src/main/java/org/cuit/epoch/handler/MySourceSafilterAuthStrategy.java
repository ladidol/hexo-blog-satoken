package org.cuit.epoch.handler;

import cn.dev33.satoken.filter.SaFilterAuthStrategy;
import cn.dev33.satoken.stp.StpUtil;
import lombok.extern.slf4j.Slf4j;
import org.cuit.epoch.dto.ResourceRoleDTO;
import org.cuit.epoch.dto.UserDetailDTO;
import org.cuit.epoch.dto.UserInfoDTO;
import org.cuit.epoch.exception.AppException;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.service.RedisService;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;
import org.springframework.util.AntPathMatcher;
import org.springframework.util.CollectionUtils;

import javax.annotation.PostConstruct;
import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

import static org.cuit.epoch.enums.RedisPrefixConst.USER_ONLINE;

/**
 * @author: ladidol
 * @date: 2022/11/18 16:02
 * @description: 资源角色管理策略
 */


@Component
@Slf4j
public class MySourceSafilterAuthStrategy implements SaFilterAuthStrategy {

    /**
     * 资源角色列表，用static能直接更改静态属性，以至于可以在类里面使用
     */
    private static List<ResourceRoleDTO> resourceRoleList;


    @Autowired
    private RoleMapper roleDao;

    @Resource
    private HttpServletRequest request;

    @Resource
    private RedisService redisService;


    /**
     * 加载资源角色信息
     */
    @PostConstruct // 在Spring中：Constructor >> @Autowired >> @PostConstruct
    private void loadDataSource() {
        resourceRoleList = roleDao.listResourceRoles();
        log.info("服务器 resourceRoleList 加载： " + resourceRoleList);
    }

    /**
     * 加载在线用户信息
     */
    @PostConstruct // 在Spring中：Constructor >> @Autowired >> @PostConstruct
    private void initOnlineUser() {
        redisService.set(USER_ONLINE, new HashSet<UserDetailDTO>());
        log.info("服务器启动 Redis中在线用户list初始化");
    }


    /**
     * 清空接口角色信息
     */
    public void clearDataSource() {
        resourceRoleList = null;
        log.info("服务器 resourceRoleList 清空");
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