package org.cuit.epoch.config.satoken;

import cn.dev33.satoken.stp.StpInterface;
import cn.dev33.satoken.stp.StpUtil;
import org.cuit.epoch.mapper.RoleMapper;
import org.cuit.epoch.service.RedisService;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

import java.util.ArrayList;
import java.util.List;

import static org.cuit.epoch.enums.RedisPrefixConst.USER_ROLE;

/**
 * 自定义权限验证接口扩展
 */
@Component    // 打开此注解，保证此类被springboot扫描，即可完成sa-token的自定义权限验证扩展
public class StpInterfaceImpl implements StpInterface {


    @Autowired
    private RoleMapper roleDao;


    @Autowired
    private RedisService redisService;


    /**
     * 返回一个账号所拥有的权限码集合
     */
    @Override
    public List<String> getPermissionList(Object loginId, String loginType) {
        // 本list仅做模拟，实际项目中要根据具体业务逻辑来查询权限
        List<String> list = new ArrayList<String>();
        list.add("101");
        list.add("user-add");
        list.add("user-delete");
        list.add("user-update");
        list.add("user-get");
        list.add("article-get");
        return list;
    }

    /**
     * 返回一个账号所拥有的角色标识集合
     */
    @Override
    public List<String> getRoleList(Object loginId, String loginType) {
        // 从redis中查询当前用户的角色列表
//        List<String> list = (List<String>) redisService.get(USER_ROLE + loginId);
        //从session中共拿角色
        List<String> list = (List<String>) StpUtil.getSession().get(USER_ROLE);
        return list;
    }

}
